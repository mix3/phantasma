package apis

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/appc/spec/schema"
	"github.com/appc/spec/schema/types"
	"github.com/coreos/go-systemd/dbus"
	"github.com/mix3/phantasma/forms"
	"github.com/mix3/phantasma/options"
	"github.com/mix3/phantasma/rkt/api/v1alpha"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

type Api struct {
	apiEndpoint string
	grpcConn    *grpc.ClientConn
	apiClient   v1alpha.PublicAPIClient
	dbusConn    *dbus.Conn
	opts        options.Options
}

func New(opts options.Options) (*Api, error) {
	grpcConn, err := grpc.Dial(opts.ApiEndpoint, grpc.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("did not connect: grpc %v", err)
	}
	dbusConn, err := dbus.New()
	if err != nil {
		return nil, fmt.Errorf("did not connect: dbus %v", err)
	}
	return &Api{
		apiEndpoint: opts.ApiEndpoint,
		grpcConn:    grpcConn,
		apiClient:   v1alpha.NewPublicAPIClient(grpcConn),
		dbusConn:    dbusConn,
		opts:        opts,
	}, nil
}

func (api *Api) Close() {
	api.grpcConn.Close()
	api.dbusConn.Close()
}

func (api *Api) getImageById(id string) (*v1alpha.Image, error) {
	res, err := api.apiClient.InspectImage(
		context.Background(),
		&v1alpha.InspectImageRequest{
			Id: id,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not InspectImageRequest: %v", err)
	}

	image := res.GetImage()

	if image == nil {
		return nil, fmt.Errorf("image not found: id %v", id)
	}

	return image, nil
}

func (api *Api) splitImageName(imageName string) (string, string, string, error) {
	var (
		prefix  = ""
		name    = ""
		version = ""
	)
	nameAndVersion := strings.Split(imageName, ":")
	name = nameAndVersion[0]
	if 1 < len(nameAndVersion) {
		version = nameAndVersion[1]
	}
	if 0 < strings.LastIndex(name, "/") {
		prefix = name[:strings.LastIndex(name, "/")]
		name = name[strings.LastIndex(name, "/")+1:]
	}
	return prefix, name, version, nil
}

func (api *Api) getImageByName(name string) (*v1alpha.Image, error) {
	prefix, name, version, err := api.splitImageName(name)
	if err != nil {
		return nil, err
	}

	filter := &v1alpha.ImageFilter{}
	if prefix != "" {
		filter.Prefixes = []string{prefix}
	}
	if name != "" {
		filter.BaseNames = []string{name}
	}
	if version != "" {
		filter.Labels = []*v1alpha.KeyValue{&v1alpha.KeyValue{"version", version}}
	}

	res, err := api.apiClient.ListImages(
		context.Background(),
		&v1alpha.ListImagesRequest{Filter: filter},
	)
	if err != nil {
		return nil, fmt.Errorf("could not ListImagesRequest: %v", err)
	}

	images := res.GetImages()

	if images == nil || len(images) == 0 {
		return nil, fmt.Errorf("image not found: name %v", name)
	}

	if 1 < len(images) {
		return nil, fmt.Errorf("image found, but duplicated: name %v", name)
	}

	return api.getImageById(images[0].Id)
}

func (api *Api) generatePodManifest(image *v1alpha.Image, annotationMap map[string]string, env forms.Envs) (*schema.PodManifest, error) {
	imageManifest := schema.BlankImageManifest()
	if err := imageManifest.UnmarshalJSON(image.Manifest); err != nil {
		return nil, err
	}

	for _, v := range env {
		imageManifest.App.Environment.Set(v.Key, v.Val)
	}

	podManifest := schema.BlankPodManifest()

	id, err := types.NewHash(image.Id)
	if err != nil {
		return nil, err
	}

	splitName := strings.Split(imageManifest.Name.String(), "/")
	name := splitName[len(splitName)-1]

	podManifest.Apps = append(podManifest.Apps, schema.RuntimeApp{
		Name: types.ACName(name),
		Image: schema.RuntimeImage{
			Name:   &imageManifest.Name,
			ID:     *id,
			Labels: imageManifest.Labels,
		},
		App: imageManifest.App,
	})

	for k, v := range annotationMap {
		podManifest.Annotations = append(podManifest.Annotations, types.Annotation{
			Name:  types.ACIdentifier(k),
			Value: v,
		})
	}

	return podManifest, nil
}

func (api *Api) withPrefix(base string) string {
	return fmt.Sprintf("%s-%s", api.opts.Specific, base)
}

func (api *Api) tmpPath(base string) string {
	return fmt.Sprintf("%s/%s", api.opts.TmpDir, base)
}

func (api *Api) unitPath(subdomain string) string {
	return fmt.Sprintf("%s/%s", api.opts.ServiceDir, api.withPrefix(subdomain))
}

func (api *Api) createUnit(podManifest *schema.PodManifest, subdomain string) error {
	podManifestJSON, err := podManifest.MarshalJSON()
	if err != nil {
		return err
	}

	serviceName := api.withPrefix(subdomain)
	unit := []byte(fmt.Sprintf(`
[Unit]
Description=%s

[Service]
ExecStartPre=/bin/sh -c '/bin/echo \'%s\' > %s'
ExecStart=%s --insecure-options=%s run --store-only --pod-manifest=%s
KillMode=mixed
`,
		serviceName,
		string(podManifestJSON),
		api.tmpPath(serviceName+".manifest"),
		api.opts.Rkt,
		api.opts.InsecureOptions,
		api.tmpPath(serviceName+".manifest"),
	))

	tmpFile, err := ioutil.TempFile(api.opts.TmpDir, api.withPrefix(""))
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile(tmpFile.Name(), unit, 0644); err != nil {
		os.Remove(tmpFile.Name())
		return err
	}

	if err := os.Rename(tmpFile.Name(), api.unitPath(subdomain+".service")); err != nil {
		os.Remove(tmpFile.Name())
		return err
	}

	return nil
}

func (api *Api) runByImage(image *v1alpha.Image, subdomain, port, net string, env forms.Envs) error {
	podManifest, err := api.generatePodManifest(image, map[string]string{
		api.opts.Specific + "-is":        "1",
		api.opts.Specific + "-subdomain": subdomain,
		api.opts.Specific + "-port":      port,
		api.opts.Specific + "-net":       net,
	}, env)
	if err != nil {
		return err
	}

	if err := api.createUnit(podManifest, subdomain); err != nil {
		return err
	}

	if err := api.dbusConn.Reload(); err != nil {
		return err
	}

	resCh := make(chan string)
	if _, err := api.dbusConn.RestartUnit(
		api.withPrefix(subdomain+".service"),
		"replace",
		resCh,
	); err != nil {
		return err
	}

	if job := <-resCh; job != "done" {
		return fmt.Errorf("job is not done: %s", job)
	}

	log.Printf("[rktapi] start %v", subdomain)

	return nil
}

func (api *Api) RunByImageId(imageId, subdomain, port, net string, env forms.Envs) error {
	image, err := api.getImageById(imageId)
	if err != nil {
		return err
	}

	return api.runByImage(image, subdomain, port, net, env)
}

func (api *Api) RunByImageName(imageName, subdomain, port, net string, env forms.Envs) error {
	image, err := api.getImageByName(imageName)
	if err != nil {
		return err
	}

	return api.runByImage(image, subdomain, port, net, env)
}

func (api *Api) Stop(subdomain string) error {
	resCh := make(chan string)
	if _, err := api.dbusConn.StopUnit(
		api.withPrefix(subdomain+".service"),
		"replace",
		resCh,
	); err != nil {
		return err
	}

	if job := <-resCh; job != "done" {
		return fmt.Errorf("job is not done: %s", job)
	}

	log.Printf("[rktapi] stop %v", subdomain)

	return nil
}

type ImageInfo struct {
	Id      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (api *Api) ImageList() ([]ImageInfo, error) {
	res, err := api.apiClient.ListImages(context.Background(), &v1alpha.ListImagesRequest{
		Filter: &v1alpha.ImageFilter{},
	})
	if err != nil {
		return nil, fmt.Errorf("could not ListImages: %v", err)
	}
	var result []ImageInfo
	for _, v := range res.GetImages() {
		result = append(result, ImageInfo{
			Id:      v.Id,
			Name:    v.Name,
			Version: v.Version,
		})
	}
	return result, nil
}

type PodInfo struct {
	Uuid      string      `json:"uuid"`
	Image     string      `json:"image"`
	Subdomain string      `json:"subdomain"`
	Port      int         `json:"port"`
	Net       string      `json:"net"`
	Host      string      `json:"host"`
	Running   bool        `json:"running"`
	Env       []forms.Env `json:"env"`
}

func (api *Api) podToPodInfo(pod *v1alpha.Pod) PodInfo {
	podManifest := schema.BlankPodManifest()
	podManifest.UnmarshalJSON(pod.Manifest)

	info := PodInfo{
		Uuid: pod.Id,
		Image: fmt.Sprintf(
			"%s:%s",
			pod.Apps[0].Image.Name,
			pod.Apps[0].Image.Version,
		),
		Running: true,
		Env:     []forms.Env{},
	}

	for _, v := range podManifest.Annotations {
		if v.Name.String() == api.opts.Specific+"-subdomain" {
			info.Subdomain = v.Value
		}
		if v.Name.String() == api.opts.Specific+"-port" {
			info.Port, _ = strconv.Atoi(v.Value)
		}
		if v.Name.String() == api.opts.Specific+"-net" {
			info.Net = v.Value
		}
	}
	for _, v := range pod.Networks {
		if v.Name == info.Net {
			info.Host = v.Ipv4
		}
	}
	for _, v := range podManifest.Apps[0].App.Environment {
		info.Env = append(info.Env, forms.Env{
			Key: v.Name,
			Val: v.Value,
		})
	}

	return info
}

func (api *Api) PodInfoMap() (map[string]PodInfo, error) {
	res, err := api.apiClient.ListPods(
		context.Background(),
		&v1alpha.ListPodsRequest{
			Filter: &v1alpha.PodFilter{
				States: []v1alpha.PodState{v1alpha.PodState_POD_STATE_RUNNING},
				Annotations: []*v1alpha.KeyValue{
					{
						Key:   api.opts.Specific + "-is",
						Value: "1",
					},
				},
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not ListPodsRequest: %v", err)
	}

	result := make(map[string]PodInfo)
	for _, pod := range res.GetPods() {
		res, err := api.apiClient.InspectPod(
			context.Background(),
			&v1alpha.InspectPodRequest{
				Id: pod.Id,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("could not InspectPodRequest: %v", err)
		}

		inspectPod := res.GetPod()
		if inspectPod == nil {
			continue
		}

		info := api.podToPodInfo(inspectPod)

		result[info.Subdomain] = info
	}

	return result, nil
}

func (api *Api) GetPodInfo(subdomain string) (PodInfo, error) {
	res, err := api.apiClient.ListPods(
		context.Background(),
		&v1alpha.ListPodsRequest{
			Filter: &v1alpha.PodFilter{
				States: []v1alpha.PodState{v1alpha.PodState_POD_STATE_RUNNING},
				Annotations: []*v1alpha.KeyValue{
					{
						Key:   api.opts.Specific + "-subdomain",
						Value: subdomain,
					},
				},
			},
		},
	)
	if err != nil {
		return PodInfo{}, fmt.Errorf("could not ListPodsRequest: %v", err)
	}

	for _, pod := range res.GetPods() {
		res, err := api.apiClient.InspectPod(
			context.Background(),
			&v1alpha.InspectPodRequest{
				Id: pod.Id,
			},
		)
		if err != nil {
			return PodInfo{}, fmt.Errorf("could not InspectPodRequest: %v", err)
		}

		inspectPod := res.GetPod()
		if inspectPod == nil {
			continue
		}

		info := api.podToPodInfo(inspectPod)

		return info, nil
	}

	return PodInfo{
		Subdomain: subdomain,
		Running:   false,
	}, nil
}

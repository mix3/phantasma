package apps

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/mholt/binding"
	"github.com/mix3/phantasma/apis"
	"github.com/mix3/phantasma/forms"
	"github.com/mix3/phantasma/options"
	"github.com/mix3/phantasma/rproxy"
	"github.com/unrolled/render"
)

type Apps struct {
	mux    *http.ServeMux
	render *render.Render
	api    *apis.Api
	rp     *rproxy.ReverseProxy
	opts   options.Options
}

func New(api *apis.Api, opts options.Options) (*Apps, error) {
	rp, err := rproxy.New(api, opts)
	if err != nil {
		return nil, err
	}

	a := &Apps{
		mux:    http.NewServeMux(),
		render: render.New(render.Options{}),
		api:    api,
		rp:     rp,
		opts:   opts,
	}
	a.mux.HandleFunc("/api/launch", a.launch)
	a.mux.HandleFunc("/api/terminate", a.terminate)
	a.mux.HandleFunc("/api/image/list", a.imageList)
	a.mux.HandleFunc("/api/list", a.list)
	a.mux.Handle("/", http.FileServer(http.Dir(opts.StaticDir)))
	return a, nil
}

func (a *Apps) launch(w http.ResponseWriter, r *http.Request) {
	launchForm := &forms.LaunchForm{
		Port: a.opts.DefaultPort,
		Net:  a.opts.DefaultNet,
	}
	errs := binding.Bind(r, launchForm)
	if 0 < errs.Len() {
		a.renderErr(w, errs)
		return
	}

	var err error
	if launchForm.ImageId != "" {
		err = a.api.RunByImageId(
			launchForm.ImageId,
			launchForm.Subdomain,
			fmt.Sprintf("%d", launchForm.Port),
			launchForm.Net,
			launchForm.Envs,
		)
	} else {
		err = a.api.RunByImageName(
			launchForm.ImageName,
			launchForm.Subdomain,
			fmt.Sprintf("%d", launchForm.Port),
			launchForm.Net,
			launchForm.Envs,
		)
	}
	if err != nil {
		a.renderErr(w, err)
		return
	}

	a.rp.Add(launchForm.Subdomain)

	a.renderOK(w)
}

func (a *Apps) terminate(w http.ResponseWriter, r *http.Request) {
	terminateForm := new(forms.TerminateForm)
	errs := binding.Bind(r, terminateForm)
	if 0 < errs.Len() {
		a.renderErr(w, errs)
		return
	}

	if err := a.api.Stop(terminateForm.Subdomain); err != nil {
		a.renderErr(w, err)
		return
	}

	a.rp.Del(terminateForm.Subdomain)

	a.renderOK(w)
}

func (a *Apps) imageList(w http.ResponseWriter, r *http.Request) {
	imageList, err := a.api.ImageList()
	if err != nil {
		a.renderErr(w, err)
		return
	}

	a.render.JSON(w, http.StatusOK, map[string][]apis.ImageInfo{
		"result": imageList,
	})
}

func (a *Apps) list(w http.ResponseWriter, r *http.Request) {
	list, err := a.rp.List()
	if err != nil {
		a.renderErr(w, err)
		return
	}

	a.render.JSON(w, http.StatusOK, map[string][]apis.PodInfo{
		"result": list,
	})
}

func (a *Apps) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := strings.Split(r.Host, ":")[0]
	suffix := "." + a.opts.Domain

	switch {
	case host == a.opts.Domain:
		a.mux.ServeHTTP(w, r)

	case strings.HasSuffix(host, suffix):
		subdomain := strings.TrimSuffix(host, suffix)
		a.rp.ServeHTTPWithSubdomain(w, r, subdomain)

	default:
		http.NotFound(w, r)
	}
}

func (a *Apps) renderOK(w http.ResponseWriter) {
	a.render.JSON(w, http.StatusOK, map[string]string{
		"result": "ok",
	})
}

func (a *Apps) renderErr(w http.ResponseWriter, err error) {
	a.render.JSON(w, http.StatusOK, map[string]string{
		"result": err.Error(),
	})
}

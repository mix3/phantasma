package rproxy

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"

	"github.com/mix3/phantasma/apis"
	"github.com/mix3/phantasma/forms"
	"github.com/mix3/phantasma/options"
)

type ReverseProxy struct {
	api   *apis.Api
	rpMap map[string]*httputil.ReverseProxy
	opts  options.Options
}

func New(api *apis.Api, opts options.Options) (*ReverseProxy, error) {
	podInfoMap, err := api.PodInfoMap()
	if err != nil {
		return nil, err
	}

	rpMap := make(map[string]*httputil.ReverseProxy)
	for k, _ := range podInfoMap {
		rpMap[k] = nil
	}

	return &ReverseProxy{
		api:   api,
		rpMap: rpMap,
		opts:  opts,
	}, nil
}

func (rp *ReverseProxy) newReverseProxy(subdomain string) (*httputil.ReverseProxy, error) {
	podInfo, err := rp.api.GetPodInfo(subdomain)
	if err != nil {
		return nil, err
	}

	if !podInfo.Running {
		return nil, fmt.Errorf("container not running: %s", subdomain)
	}

	dest, err := url.Parse(fmt.Sprintf("http://%s:%d", podInfo.Host, podInfo.Port))
	if err != nil {
		return nil, err
	}

	return httputil.NewSingleHostReverseProxy(dest), nil
}

func (rp *ReverseProxy) ServeHTTPWithSubdomain(w http.ResponseWriter, r *http.Request, subdomain string) {
	reverseProxy, ok := rp.rpMap[subdomain]

	if !ok {
		http.NotFound(w, r)
		return
	}

	if reverseProxy != nil {
		reverseProxy.ServeHTTP(w, r)
		return
	}

	log.Println("[proxy] initialize", subdomain)

	new, err := rp.newReverseProxy(subdomain)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rp.rpMap[subdomain] = new

	new.ServeHTTP(w, r)
}

func (rp *ReverseProxy) subdomainList() []string {
	subdomains := make([]string, 0, len(rp.rpMap))
	for k, _ := range rp.rpMap {
		subdomains = append(subdomains, k)
	}
	sort.Strings(subdomains)
	return subdomains
}

func (rp *ReverseProxy) List() ([]apis.PodInfo, error) {
	podInfoMap, err := rp.api.PodInfoMap()
	if err != nil {
		return nil, err
	}

	result := []apis.PodInfo{}
	for _, subdomain := range rp.subdomainList() {
		if v, ok := podInfoMap[subdomain]; ok {
			result = append(result, v)
		} else {
			result = append(result, apis.PodInfo{
				Subdomain: subdomain,
				Running:   false,
				Env:       []forms.Env{},
			})
		}
	}

	return result, nil
}

func (rp *ReverseProxy) Add(subdomain string) {
	log.Println("[proxy] add proxy", subdomain)

	rp.rpMap[subdomain] = nil
}

func (rp *ReverseProxy) Del(subdomain string) {
	log.Println("[proxy] del proxy", subdomain)

	delete(rp.rpMap, subdomain)
}

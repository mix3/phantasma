package forms

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/mholt/binding"
)

type Env struct {
	Key string `json:"key"`
	Val string `json:"val"`
}

type Envs []Env

func (e *Envs) Bind(fieldName string, strVals []string, errs binding.Errors) binding.Errors {
	for _, v := range strVals {
		kv := strings.Split(v, "=")
		if len(kv) != 2 {
			errs.Add([]string{fieldName}, "EnvParseError", fmt.Sprintf("cannot parse Env for: %v", v))
		}
		*e = append(*e, Env{
			Key: kv[0],
			Val: kv[1],
		})
	}
	return errs
}

func (e Envs) OptionString() string {
	var opts []string
	for _, v := range e {
		opts = append(opts, fmt.Sprintf(`--set-env=%s="%s"`, v.Key, v.Val))
	}
	return strings.Join(opts, " ")
}

var subdomainMatcher = regexp.MustCompile("^[a-zA-Z0-9-.]+$")

type LaunchForm struct {
	ImageId   string
	ImageName string
	Subdomain string
	Port      int
	Net       string
	Envs      Envs
}

func (lf *LaunchForm) FieldMap(r *http.Request) binding.FieldMap {
	return binding.FieldMap{
		&lf.ImageId: binding.Field{
			Form: "image_id",
		},
		&lf.ImageName: binding.Field{
			Form: "image_name",
		},
		&lf.Subdomain: binding.Field{
			Form:     "subdomain",
			Required: true,
		},
		&lf.Port: binding.Field{
			Form: "port",
		},
		&lf.Net: binding.Field{
			Form: "net",
		},
		&lf.Envs: binding.Field{
			Form: "env",
		},
	}
}

func (lf LaunchForm) Validate(r *http.Request, errs binding.Errors) binding.Errors {
	if lf.ImageId == "" && lf.ImageName == "" {
		errs = append(errs, binding.Error{
			FieldNames:     []string{"image_id", "image_name"},
			Classification: binding.RequiredError,
			Message:        "require image_id or image_name",
		})
	}
	if !subdomainMatcher.MatchString(lf.Subdomain) {
		errs = append(errs, binding.Error{
			FieldNames:     []string{"subdomain"},
			Classification: "RegExpError",
			Message:        "subdomain is not good",
		})
	}
	return errs
}

type TerminateForm struct {
	Subdomain string
}

func (tf *TerminateForm) FieldMap(r *http.Request) binding.FieldMap {
	return binding.FieldMap{
		&tf.Subdomain: binding.Field{
			Form:     "subdomain",
			Required: true,
		},
	}
}

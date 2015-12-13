package options

type Options struct {
	Host            string `short:"h" long:"host" default:"127.0.0.1" description:"server host"`
	Port            int    `short:"p" long:"port" default:"5000" description:"server port"`
	ApiEndpoint     string `long:"api-endpoint" default:"localhost:15441" description:"rkt api endpoint"`
	DefaultPort     int    `long:"default-port" default:"5000" description:"reverse proxy default port"`
	DefaultNet      string `long:"default-net" default:"default" description:"reverse proxy default net"`
	Domain          string `long:"domain" required:"true" description:"reverse proxy domain"`
	InsecureOptions string `long:"insecure-options" default:"image" description:"rkt option"`
	TmpDir          string `long:"tmp-dir" default:"/tmp" description:"tmp dir"`
	Specific        string `long:"specific" default:"phantasma" description:"specific for prefix, suffix"`
	ServiceDir      string `long:"service-dir" default:"/etc/systemd/system" description:"systemd service dir"`
	Rkt             string `long:"rkt" default:"/usr/local/bin/rkt" description:"rkt command path"`
	StaticDir       string `long:"static-dir" default:"." description:"static file server dir"`
}

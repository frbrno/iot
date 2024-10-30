package node

import (
	"flag"
	"fmt"
	"os"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	ID     string         `yaml:"id" env:"ID" env-description:"has to be the same as the mcu is using"`
	Ctrler any            `yaml:"ctrler"`
	Nodes  map[string]any `yaml:"nodes"`
}

// Args command-line parameters
type ConfigArgs struct {
	ConfigPath string
	ID         string
}

// ProcessArgs processes and handles CLI arguments
func ConfigLoad(cfg any) (string, error) {
	var a ConfigArgs

	f := flag.NewFlagSet("Example server", 1)
	f.StringVar(&a.ConfigPath, "cfg", "config.yml", "Path to configuration file")
	f.StringVar(&a.ID, "id", "rusty_falcon", "id of mcu")

	fu := f.Usage
	f.Usage = func() {
		fu()
		envHelp, _ := cleanenv.GetDescription(cfg, nil)
		fmt.Fprintln(f.Output())
		fmt.Fprintln(f.Output(), envHelp)
	}

	f.Parse(os.Args[1:])

	return a.ID, cleanenv.ReadConfig(a.ConfigPath, cfg)
}

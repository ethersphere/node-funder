package funder

import (
	"flag"
	"fmt"
)

type Config struct {
	Namespace string
	MinETH    float64
	MinBZZ    float64
	MinGBZZ   float64
}

func ParseConfig() (Config, error) {
	cfg := Config{}

	flag.StringVar(&cfg.Namespace, "namespace", "", "kuberneties namespace")
	flag.Float64Var(&cfg.MinETH, "minETH", 0, "specifies min amout of ETH tokens nodes should have")
	flag.Float64Var(&cfg.MinBZZ, "minBZZ", 0, "specifies min amout of BZZ tokens nodes should have")
	flag.Float64Var(&cfg.MinGBZZ, "minGBZZ", 0, "specifies min amout of GBZZ tokens nodes should have")
	flag.Parse()

	if cfg.Namespace == "" {
		return cfg, fmt.Errorf("namespace must be set")
	}

	return cfg, nil
}

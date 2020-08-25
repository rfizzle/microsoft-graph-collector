package main

import (
	"errors"
	"github.com/rfizzle/collector-helpers/config"
	"github.com/rfizzle/collector-helpers/outputs"
	"github.com/rfizzle/collector-helpers/state"
	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"
	"strings"
)

func setupCliFlags() error {
	viper.SetEnvPrefix("MICROSOFT_GRAPH_COLLECTOR")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	config.InitCLIParams()
	flag.Int("schedule", 30, "time in seconds to collect")
	flag.String("tenant-id", "", "tenant id")
	flag.String("client-id", "", "client id")
	flag.String("client-secret", "", "client secret")
	flag.BoolP("verbose", "v", false, "verbose logging")
	state.InitCLIParams()
	outputs.InitCLIParams()
	flag.Parse()
	err := viper.BindPFlags(flag.CommandLine)

	if err != nil {
		return err
	}

	// Check config
	if err := config.CheckConfigParams(); err != nil {
		return err
	}

	// Check parameters
	if err := checkRequiredParams(); err != nil {
		return err
	}

	return nil
}

func checkRequiredParams() error {
	if viper.GetString("tenant-id") == "" {
		return errors.New("missing tenant id param (--tenant-id)")
	}

	if viper.GetString("client-id") == "" {
		return errors.New("missing client id param (--client-id)")
	}

	if viper.GetString("client-secret") == "" {
		return errors.New("missing client secret param (--client-secret)")
	}

	if err := state.ValidateCLIParams(); err != nil {
		return err
	}

	if err := outputs.ValidateCLIParams(); err != nil {
		return err
	}

	return nil
}

package cli

import (
	"flag"
	"fmt"
	"time"

	"github.com/fikrilal/agy-auth/internal/registry"
)

const (
	defaultLiveIntervalSeconds = 60
	minLiveIntervalSeconds     = 5
	maxLiveIntervalSeconds     = 3600
	liveDefaultInterval        = time.Duration(defaultLiveIntervalSeconds) * time.Second
)

func (a *App) cmdConfig(args []string) int {
	if len(args) == 0 {
		return a.cmdConfigLive(nil)
	}
	switch args[0] {
	case "live":
		return a.cmdConfigLive(args[1:])
	case "-h", "--help":
		a.printCommandHelp("config")
		return 0
	default:
		return a.usageFor("config", "unknown config section %q", args[0])
	}
}

func (a *App) cmdConfigLive(args []string) int {
	fs := a.newFlagSet("config")
	interval := fs.Int("interval", defaultLiveIntervalSeconds, "live refresh interval in seconds (5-3600)")
	if ok, code := a.parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return a.usageFor("config", "config live takes no positional arguments")
	}
	set := false
	fs.Visit(func(value *flag.Flag) {
		if value.Name == "interval" {
			set = true
		}
	})

	reg, err := a.Store.Load()
	if err != nil {
		return a.registryFailure(err, "could not read the account registry")
	}
	if !set {
		seconds := effectiveLiveIntervalSeconds(reg)
		if a.JSON {
			if err := a.emitJSON(map[string]int{"live_interval_seconds": seconds}); err != nil {
				return a.failCode("output_error", "", "could not write JSON output")
			}
			return 0
		}
		fmt.Fprintf(a.Out, "Live refresh interval: %ds.\n", seconds)
		return 0
	}
	if *interval < minLiveIntervalSeconds || *interval > maxLiveIntervalSeconds {
		return a.usageFor("config", "live interval must be between %d and %d seconds", minLiveIntervalSeconds, maxLiveIntervalSeconds)
	}
	if reg.Config == nil {
		reg.Config = &registry.Config{}
	}
	reg.Config.LiveIntervalSeconds = *interval
	if reg.Accounts == nil {
		reg.Accounts = []registry.Account{}
	}
	if err := a.Store.Save(reg); err != nil {
		return a.registryFailure(err, "could not save the live interval")
	}
	if a.JSON {
		if err := a.emitJSON(map[string]int{"live_interval_seconds": *interval}); err != nil {
			return a.failCode("output_error", "", "could not write JSON output")
		}
		return 0
	}
	fmt.Fprintf(a.Out, "Live refresh interval set to %ds.\n", *interval)
	return 0
}

func effectiveLiveIntervalSeconds(reg *registry.Registry) int {
	value := reg.LiveIntervalSeconds()
	if value < minLiveIntervalSeconds || value > maxLiveIntervalSeconds {
		return defaultLiveIntervalSeconds
	}
	return value
}

func (a *App) liveInterval() time.Duration {
	reg, err := a.Store.Load()
	if err != nil {
		return liveDefaultInterval
	}
	return time.Duration(effectiveLiveIntervalSeconds(reg)) * time.Second
}

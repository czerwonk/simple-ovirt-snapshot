package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"regexp"

	"github.com/czerwonk/oSnap/api"
)

const version = "0.3.0"

var (
	showVersion     = flag.Bool("version", false, "Print version information")
	apiURL          = flag.String("api.url", "https://localhost/ovirt-engine/api/", "API REST Endpoint")
	apiUser         = flag.String("api.user", "user@internal", "API username")
	apiPass         = flag.String("api.pass", "", "API password")
	apiInsecureCert = flag.Bool("api.insecure-cert", false, "Skip verification for untrusted SSL/TLS certificates")
	cluster         = flag.String("cluster", "", "Cluster name to filter")
	vm              = flag.String("vm", "", "VM name(s) to snapshot (regex)")
	skip            = flag.String("skip", "", "VM name(s) to skip (regex)")
	desc            = flag.String("desc", "oSnap generated snapshot", "Description to use for the snapshot")
	keep            = flag.Int("keep", 7, "Number of snapshots to keep")
	debug           = flag.Bool("debug", false, "Prints API requests and responses to STDOUT")
	purgeOnly       = flag.Bool("purge-only", false, "Only deleting old snapshots without creating a new one")
)

func init() {
	flag.Usage = func() {
		fmt.Println("Usage: oSnap [ ... ]\n\nParameters:")
		fmt.Println()
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()

	if *showVersion {
		printVersion()
		os.Exit(0)
	}

	err := run()
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
}

func printVersion() {
	fmt.Println("oSnap - oVirt Snapshot Creator")
	fmt.Printf("Version: %s\n", version)
	fmt.Println("Author(s): Daniel Czerwonk")
}

func run() error {
	a, err := getAPI()
	if err != nil {
		return err
	}

	vms, err := a.GetVms()
	if err != nil {
		return err
	}

	var snapped []api.Vm
	if !*purgeOnly {
		snapped = createSnapshots(vms, a)
	}

	var success int
	if *purgeOnly {
		success = purgeOldSnapshots(vms, a)
	} else {
		success = purgeOldSnapshots(snapped, a)
	}

	if success != len(vms) {
		return fmt.Errorf("One or more errors occurred. See output above for more detail.")
	}

	return nil
}

func getAPI() (*api.Api, error) {
	a, err := api.New(*apiURL, *apiUser, *apiPass, *apiInsecureCert, *debug)
	if err != nil {
		return nil, err
	}

	a.ClusterFilter = *cluster

	if len(*vm) > 0 {
		a.VMFilter, err = regexp.Compile(*vm)
		if err != nil {
			return nil, err
		}
	}

	if len(*skip) > 0 {
		a.SkipFilter, err = regexp.Compile(*skip)
		if err != nil {
			return nil, err
		}
	}

	return a, nil
}

func createSnapshots(vms []api.Vm, a *api.Api) []api.Vm {
	snapshots := make([]*api.Snapshot, 0)
	for _, vm := range vms {
		log.Printf("%s: Creating snapshot for VM", vm.Name)
		s, err := a.CreateSnapshot(vm.ID, *desc)
		if err != nil {
			log.Printf("%s: Snapshot failed - %v)\n", vm.Name, err)
		}

		snapshots = append(snapshots, s)
		log.Printf("%s: Snapshot job created. (ID: %s)\n", vm.Name, s.ID)
	}

	return monitorSnapshotCreation(snapshots, a)
}

func monitorSnapshotCreation(snapshots []*api.Snapshot, a *api.Api) []api.Vm {
	complete := make([]api.Vm, 0)

	for _, s := range snapshots {
		x, err := waitForCompletion(s, a)
		if err != nil {
			log.Printf("%s: Snapshot failed - %v)\n", s.VM.Name, err)
		} else {
			log.Printf("%s: Snapshot completed\n", x.VM.Name)
			complete = append(complete, x.VM)
		}
	}

	return complete
}

func waitForCompletion(snapshot *api.Snapshot, a *api.Api) (*api.Snapshot, error) {
	log.Printf("Waiting for snapshot %s to finish...\n", snapshot.ID)

	for {
		s, err := a.GetSnapshot(snapshot.VM.ID, snapshot.ID)
		if err != nil {
			return nil, err
		}

		if strings.HasPrefix(s.Status, "fail") || strings.HasPrefix(s.Status, "error") {
			return nil, fmt.Errorf(s.Status)
		}

		if s.Status == "ok" {
			return s, nil
		}

		time.Sleep(30 * time.Second)
	}
}

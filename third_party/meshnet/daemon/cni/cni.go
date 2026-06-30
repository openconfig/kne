package cni

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/containernetworking/cni/libcni"
	"github.com/containernetworking/cni/pkg/types"
	log "github.com/sirupsen/logrus"
)

const (
	defaultNetDir     = "/etc/cni/net.d"
	defaultCNIFile    = "00-meshnet.conflist"
	interNodeLinkConf = "/etc/cni/net.d/meshnet-inter-node-link-type"
	defaultPluginName = "meshnet"
)

var meshnetCNIPath = filepath.Join(defaultNetDir, defaultCNIFile)

// This is borrowed from https://tinyurl.com/khjhf9xd
func loadConfList() (map[string]interface{}, error) {
	files, err := libcni.ConfFiles(defaultNetDir, []string{".conf", ".conflist", ".json"})
	switch {
	case err != nil:
		return nil, err
	case len(files) == 0:
		return nil, libcni.NoConfigsFoundError{Dir: defaultNetDir}
	}

	// Ignore any existing meshnet config files
	var confFiles []string
	for _, f := range files {
		if strings.Contains(f, "meshnet") {
			continue
		}
		confFiles = append(confFiles, f)
	}

	sort.Strings(confFiles)
	// Iterate over existing confFiles and pick the first one that's valid, borrowed from https://tinyurl.com/977uyx5m
	for _, confFile := range confFiles {
		var confList *libcni.NetworkConfigList
		if strings.HasSuffix(confFile, ".conflist") {
			confList, err = libcni.ConfListFromFile(confFile)
			if err != nil {
				log.Infof("Error loading %q CNI config list file: %s", confFile, err)
				continue
			}
		} else {
			conf, err := libcni.ConfFromFile(confFile)
			if err != nil {
				log.Infof("Error loading %q CNI config file: %s", confFile, err)
				continue
			}
			// Ensure the config has a "type" so we know what plugin to run.
			// Also catches the case where somebody put a conflist into a conf file.
			if conf.Network.Type == "" {
				log.Infof("Error loading %q CNI config file: no 'type'; perhaps this is a .conflist?", confFile)
				continue
			}

			confList, err = libcni.ConfListFromConf(conf)
			if err != nil {
				log.Infof("Error converting CNI config file %q to list: %s", confFile, err)
				continue
			}
		}
		if len(confList.Plugins) == 0 {
			log.Infof("%q CNI config list has no plugins, skipping", confFile)
			continue
		}

		// only pre-parse the top of the CNI file without using the types.NetConfList
		// this is because some generic types do not define the complete config struct
		// e.g. IPAM config will not be parsed at all beyong the `type`
		var conf map[string]interface{}
		err = json.Unmarshal(confList.Bytes, &conf)

		return conf, err
	}

	return nil, fmt.Errorf("no valid network configurations found in %q", defaultNetDir)
}

func saveConfList(m map[string]interface{}) error {
	bytes, err := json.MarshalIndent(m, "", "\t")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(meshnetCNIPath, bytes, os.FileMode(06444))
}

func saveInterNodeLinkConf() error {
	return ioutil.WriteFile(interNodeLinkConf, []byte(os.Getenv("INTER_NODE_LINK_TYPE")), os.FileMode(06444))
}

func removeInterNodeLinkConf() error {
	if err := os.Remove(interNodeLinkConf); err != nil {
		return fmt.Errorf("failed to remove %s: %v", interNodeLinkConf, err)
	}
	return nil
}

// Init installs meshnet CNI configuration
func Init() error {

	conf, err := loadConfList()
	if err != nil {
		return err
	}

	// We can safely access and type-cast since all of the checks have already been done in the `loadConfList()`
	plugins := conf["plugins"].([]interface{})

	plugins = append(plugins, &types.NetConf{
		Type: defaultPluginName,
		Name: defaultPluginName,
	})

	conf["cniVersion"] = "0.3.0"
	conf["plugins"] = plugins

	// TODO: check if we can avoid creating a custom file for propagating value of env INTER_NODE_LINK_TYPE
	if err := saveInterNodeLinkConf(); err != nil {
		return err
	}

	return saveConfList(conf)
}

// Cleanup removes meshnet CNI configuration
func Cleanup() {
	if err := os.Remove(meshnetCNIPath); err != nil {
		log.Infof("Failed to remove file %s: %v", meshnetCNIPath, err)
	}
	if err := removeInterNodeLinkConf(); err != nil {
		log.Infof("Failed to remove inter node link conf: %v", err)
	}
}

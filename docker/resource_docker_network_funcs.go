package docker

import (
	"fmt"

	"context"
	"encoding/json"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
	"log"
	"time"
)

func resourceDockerNetworkCreate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ProviderConfig).DockerClient

	createOpts := types.NetworkCreate{}
	if v, ok := d.GetOk("check_duplicate"); ok {
		createOpts.CheckDuplicate = v.(bool)
	}
	if v, ok := d.GetOk("driver"); ok {
		createOpts.Driver = v.(string)
	}
	if v, ok := d.GetOk("options"); ok {
		createOpts.Options = mapTypeMapValsToString(v.(map[string]interface{}))
	}
	if v, ok := d.GetOk("internal"); ok {
		createOpts.Internal = v.(bool)
	}

	ipamOpts := &network.IPAM{}
	ipamOptsSet := false
	if v, ok := d.GetOk("ipam_driver"); ok {
		ipamOpts.Driver = v.(string)
		ipamOptsSet = true
	}
	if v, ok := d.GetOk("ipam_config"); ok {
		ipamOpts.Config = ipamConfigSetToIpamConfigs(v.(*schema.Set))
		ipamOptsSet = true
	}

	if ipamOptsSet {
		createOpts.IPAM = ipamOpts
	}

	retNetwork := types.NetworkCreateResponse{}
	retNetwork, err := client.NetworkCreate(context.Background(), d.Get("name").(string), createOpts)
	if err != nil {
		return fmt.Errorf("Unable to create network: %s", err)
	}

	d.SetId(retNetwork.ID)

	return resourceDockerNetworkRead(d, meta)
}

func resourceDockerNetworkRead(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[INFO] Waiting for network: '%s' to expose all fields: max '%v seconds'", d.Id(), 30)

	stateConf := &resource.StateChangeConf{
		Pending:    []string{"pending"},
		Target:     []string{"all_fields", "removed"},
		Refresh:    resourceDockerNetworkReadRefreshFunc(d, meta),
		Timeout:    30 * time.Second,
		MinTimeout: 5 * time.Second,
		Delay:      2 * time.Second,
	}

	// Wait, catching any errors
	_, err := stateConf.WaitForState()
	if err != nil {
		return err
	}

	return nil
}

func resourceDockerNetworkDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ProviderConfig).DockerClient

	if err := client.NetworkRemove(context.Background(), d.Id()); err != nil {
		return fmt.Errorf("Error deleting network %s: %s", d.Id(), err)
	}

	d.SetId("")
	return nil
}

func ipamConfigSetToIpamConfigs(ipamConfigSet *schema.Set) []network.IPAMConfig {
	ipamConfigs := make([]network.IPAMConfig, ipamConfigSet.Len())

	for i, ipamConfigInt := range ipamConfigSet.List() {
		ipamConfigRaw := ipamConfigInt.(map[string]interface{})

		ipamConfig := network.IPAMConfig{}
		ipamConfig.Subnet = ipamConfigRaw["subnet"].(string)
		ipamConfig.IPRange = ipamConfigRaw["ip_range"].(string)
		ipamConfig.Gateway = ipamConfigRaw["gateway"].(string)

		auxAddressRaw := ipamConfigRaw["aux_address"].(map[string]interface{})
		ipamConfig.AuxAddress = make(map[string]string, len(auxAddressRaw))
		for k, v := range auxAddressRaw {
			ipamConfig.AuxAddress[k] = v.(string)
		}

		ipamConfigs[i] = ipamConfig
	}

	return ipamConfigs
}

func resourceDockerNetworkReadRefreshFunc(
	d *schema.ResourceData, meta interface{}) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		client := meta.(*ProviderConfig).DockerClient
		networkID := d.Id()

		retNetwork, _, err := client.NetworkInspectWithRaw(context.Background(), networkID, types.NetworkInspectOptions{})
		if err != nil {
			log.Printf("[WARN] Network (%s) not found, removing from state", networkID)
			d.SetId("")
			return networkID, "removed", err
		}

		jsonObj, _ := json.MarshalIndent(retNetwork, "", "\t")
		log.Printf("[DEBUG] Docker network inspect: %s", jsonObj)

		d.Set("internal", retNetwork.Internal)
		if len(retNetwork.Driver) > 0 {
			d.Set("driver", retNetwork.Driver)
		} else {
			log.Printf("[DEBUG] driver: %v not exposed", retNetwork.Driver)
			return networkID, "pending", nil
		}
		if len(retNetwork.Scope) > 0 {
			d.Set("scope", retNetwork.Scope)
		} else {
			log.Printf("[DEBUG] scope: %v not exposed", retNetwork.Scope)
			return networkID, "pending", nil
		}
		if retNetwork.Options != nil && len(retNetwork.Options) != 0 {
			d.Set("options", retNetwork.Options)
		} else {
			log.Printf("[DEBUG] options: %v not exposed", retNetwork.Options)
			return networkID, "pending", nil
		}

		log.Println("[DEBUG] all network fields exposed")
		return networkID, "all_fields", nil
	}
}

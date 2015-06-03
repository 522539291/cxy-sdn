package server

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/WIZARD-CXY/cxy-sdn/util"
	"github.com/WIZARD-CXY/netAgent"
	"math"
	"net"
)

const networkStore = "network"
const vlanStore = "vlan"
const ipStore = "ip"
const defaultNetwork = "default"

var gatewayAddrs = []string{
	// Here we don't follow the convention of using the 1st IP of the range for the gateway.
	// This is to use the same gateway IPs as the /24 ranges, which predate the /16 ranges.
	// In theory this shouldn't matter - in practice there's bound to be a few scripts relying
	// on the internal addressing or other stupid things like that.
	// They shouldn't, but hey, let's not break them unless we really have to.
	"10.1.42.1/16",
	"10.42.42.1/16",
	"172.16.42.1/24",
	"172.16.43.1/24",
	"172.16.44.1/24",
	"10.0.42.1/24",
	"10.0.43.1/24",
	"172.17.42.1/16", // Don't use 172.16.0.0/16, it conflicts with EC2 DNS 172.16.0.23
	"10.0.42.1/16",   // Don't even try using the entire /8, that's too intrusive
	"192.168.42.1/24",
	"192.168.43.1/24",
	"192.168.44.1/24",
}

const vlanCount = 4096

type Network struct {
	Name    string `json:"name"`
	Subnet  string `json:"subnet"`
	Gateway string `json:"gateway"`
	VlanID  uint   `json:"vlanid"`
}

// get the network detail of a given name
func GetNetwork(name string) (*Network, error) {
	netByte, _, ok := netAgent.Get(networkStore, name)
	if ok {
		network := &Network{}
		err := json.Unmarshal(netByte, network)

		if err != nil {
			return nil, err
		}

		return network, nil
	}
	return nil, errors.New("Network " + name + " not exist")
}

func GetNetworks() ([]Network, error) {
	networkBytes, _, _ := netAgent.GetAll(networkStore)
	networks := make([]Network, 0)

	for _, networkByte := range networkBytes {
		network := Network{}
		err := json.Unmarshal(networkByte, &network)

		if err != nil {
			return nil, err
		}

		networks = append(networks, network)
	}
	return networks, nil
}
func CreateNetwork(name string, subnet *net.IPNet) (*Network, error) {
	network, err := GetNetwork(name)

	if err == nil {
		return network, nil
	}

	// get the smallest unused vlan id from data store
	vlanID, err := allocateVlan()

	if err != nil {
		return nil, err
	}

	var gateway net.IP

	_, err = util.GetIfaceAddr(name)

	if err != nil {

		/*if ovs == nil {
			return nil, errors.New("OVS not connected")
		}*/

		gateway = RequestIP(*subnet)
		network = &Network{name, subnet.String(), gateway.String(), vlanID}
	}

	return &Network{}, nil

}

func CreateDefaultNetwork() (*Network, error) {
	//CreateNetwork(defaultNetwork, subnet)
	return &Network{}, nil
}

func DeleteNetwork(name string) error {
	network, err := GetNetwork(name)
	if err != nil {
		return err
	}
	/*if ovs == nil {
		return errors.New("OVS not connected")
	}*/
	eccerror := netAgent.Delete(networkStore, name)
	if eccerror != netAgent.OK {
		return errors.New("Error deleting network")
	}
	releaseVlan(network.VlanID)
	//deletePort(ovs, defaultBridgeName, name)
	return nil
}

func allocateVlan() (uint, error) {
	vlanBytes, _, ok := netAgent.Get(vlanStore, "vlan")

	// not put the key already
	if !ok {
		vlanBytes = make([]byte, vlanCount/8)
	}

	curVal := make([]byte, vlanCount/8)
	copy(curVal, vlanBytes)

	vlanID := util.TestAndSet(vlanBytes)

	if vlanID > vlanCount {
		return vlanID, errors.New("All vlanID have been used")
	}

	netAgent.Put(vlanStore, "vlan", vlanBytes, curVal)

	return vlanID, nil

}

func releaseVlan(vlanID uint) {
	oldVal, _, ok := netAgent.Get(vlanStore, "vlan")

	if !ok {
		oldVal = make([]byte, vlanCount/8)
	}
	curVal := make([]byte, vlanCount/8)
	copy(curVal, oldVal)

	util.Clear(curVal, vlanID-1)
	err := netAgent.Put(vlanStore, "vlan", curVal, oldVal)
	if err == netAgent.OUTDATED {
		releaseVlan(vlanID)
	}
}
func GetAvailableGwAddress(bridgeIP string) (gwaddr string, err error) {
	if len(bridgeIP) != 0 {
		_, _, err = net.ParseCIDR(bridgeIP)
		if err != nil {
			return
		}
		gwaddr = bridgeIP
	} else {
		for _, addr := range gatewayAddrs {
			_, dockerNetwork, err := net.ParseCIDR(addr)
			if err != nil {
				return "", err
			}
			if err = util.CheckRouteOverlaps(dockerNetwork); err != nil {
				continue
			}
			gwaddr = addr
			break
		}
	}
	if gwaddr == "" {
		return "", errors.New("No available gateway addresses")
	}
	return gwaddr, nil
}

func GetAvailableSubnet() (subnet *net.IPNet, err error) {
	for _, addr := range gatewayAddrs {
		_, dockerNetwork, err := net.ParseCIDR(addr)
		if err != nil {
			return &net.IPNet{}, err
		}
		if err = util.CheckRouteOverlaps(dockerNetwork); err == nil {
			return dockerNetwork, nil
		}
	}

	return &net.IPNet{}, errors.New("No available GW address")
}

// ipStore manage the cluster ip resource
// key is the subnet, value is the available ip address

// Get an IP from the subnet and mark it as used
func RequestIP(subnet net.IPNet) net.IP {
	ipCount := util.IPCount(subnet)
	bc := int(ipCount / 8)
	partial := int(math.Mod(ipCount, float64(8)))

	if partial != 0 {
		bc += 1
	}

	oldArray, _, ok := netAgent.Get(ipStore, subnet.String())

	if !ok {
		oldArray = make([]byte, bc)
	}

	newArray := make([]byte, len(oldArray))

	copy(newArray, oldArray)

	pos := util.TestAndSet(newArray)

	err := netAgent.Put(ipStore, subnet.String(), newArray, oldArray)

	if err == netAgent.OUTDATED {
		return RequestIP(subnet)
	}

	var num uint

	buf := bytes.NewBuffer(subnet.IP)

	err2 := binary.Read(buf, binary.BigEndian, &num)

	if err2 != nil {
		return nil
	}

	num += pos

	buf2 := new(bytes.Buffer)
	err2 = binary.Write(buf2, binary.BigEndian, num)

	if err2 != nil {
		return nil
	}
	return net.IP(buf2.Bytes())

}

// Release the given IP from the subnet
func ReleaseIP(addr net.IP, subnet net.IPNet) bool {
	oldArray, _, ok := netAgent.Get(ipStore, subnet.IP.String())

	if !ok {
		return false
	}

	newArray := make([]byte, len(oldArray))
	copy(newArray, oldArray)

	var num1, num2 uint
	buf1 := bytes.NewBuffer(oldArray)
	binary.Read(buf1, binary.BigEndian, &num1)

	buf := bytes.NewBuffer(subnet.IP)

	binary.Read(buf, binary.BigEndian, &num2)

	pos := num1 - num2 - 1

	util.Clear(newArray, pos)

	err2 := netAgent.Put(ipStore, subnet.String(), newArray, oldArray)

	if err2 == netAgent.OUTDATED {
		return ReleaseIP(addr, subnet)
	}

	return true
}

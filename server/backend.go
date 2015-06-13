package server

import (
	"github.com/WIZARD-CXY/cxy-sdn/util"
	"github.com/WIZARD-CXY/netAgent"
	"os"
)

const dataDir = "/tmp/cxy/"

type Listener struct{}

var listener Listener

func InitAgent(bindInterface string, bootstrap bool) error {
	// simple set up
	// if one is started as bootstrap node, start it in serverMode
	err := netAgent.StartAgent(bootstrap, bootstrap, bindInterface, dataDir)

	if err == nil {
		go netAgent.RegisterForNodeUpdates(listener)
	}
	return err
}

func join(addr string) error {
	return netAgent.Join(addr)
}

func leave() error {
	if err := netAgent.Leave(); err != nil {
		//glog.Error(err)
		return err
	}

	//clean the data storage
	if err := os.RemoveAll(dataDir); err != nil {
		// glog.Error(err)
		return err
	}

	return nil
}

func (l Listener) NotifyNodeUpdate(nType netAgent.NotifyUpdateType, nodeAddr string) {
	if nType == netAgent.NOTIFY_UPDATE_ADD {
		// glog.Infof("New node %s joined in", nodeAddr)
		myIp, _ := util.MyIP()
		if nodeAddr != myIp {
			// add tunnel to the other node
			AddPeer(nodeAddr)
		}
	} else if nType == netAgent.NOTIFY_UPDATE_DELETE {
		// glog.Infof("Node %s left", nodeAddr)
		DeletePeer(nodeAddr)
	}
}

func (l Listener) NotifyKeyUpdate(nType netAgent.NotifyUpdateType, key string, data []byte) {
	// do nothing
}

func (l Listener) NotifyStoreUpdate(nType netAgent.NotifyUpdateType, store string, data map[string][]byte) {
	// do nothing
}
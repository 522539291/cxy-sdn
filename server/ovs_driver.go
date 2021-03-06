package server

import (
	"errors"
	"fmt"
	"log"
	"reflect"
	"time"

	"github.com/socketplane/libovsdb"
)

var quit chan bool
var update chan *libovsdb.TableUpdates
var cache map[string]map[string]libovsdb.Row

const CONTEXT_KEY = "container_id"
const CONTEXT_VALUE = "container_data"

func GetTableCache(tableName string) map[string]libovsdb.Row {
	return cache[tableName]
}

func monitorDockerBridge(ovsClient *libovsdb.OvsdbClient) {
	for {
		select {
		case currUpdate := <-update:
			for table, tableUpdate := range currUpdate.Updates {
				if table == "Bridge" {
					for _, row := range tableUpdate.Rows {
						empty := libovsdb.Row{}
						if !reflect.DeepEqual(row.New, empty) {
							oldRow := row.Old
							if _, ok := oldRow.Fields["name"]; ok {
								name := oldRow.Fields["name"].(string)
								if name == "ovs-br0" {
									CreateOVSBridge(ovsClient, name)
								}
							}
						}
					}
				}
			}
		}
	}
}

func CreateOVSBridge(ovsClient *libovsdb.OvsdbClient, bridgeName string) (string, error) {
	namedBridgeUuid := "bridge"
	namedPortUuid := "port"
	namedIntfUuid := "intf"

	// intf row to insert
	intf := make(map[string]interface{})
	intf["name"] = bridgeName
	intf["type"] = `internal`

	insertIntfOp := libovsdb.Operation{
		Op:       "insert",
		Table:    "Interface",
		Row:      intf,
		UUIDName: namedIntfUuid,
	}

	// port row to insert
	port := make(map[string]interface{})
	port["name"] = bridgeName
	port["interfaces"] = libovsdb.UUID{namedIntfUuid}

	insertPortOp := libovsdb.Operation{
		Op:       "insert",
		Table:    "Port",
		Row:      port,
		UUIDName: namedPortUuid,
	}

	// bridge row to insert
	bridge := make(map[string]interface{})
	bridge["name"] = bridgeName
	bridge["stp_enable"] = true
	bridge["ports"] = libovsdb.UUID{namedPortUuid}

	insertBridgeOp := libovsdb.Operation{
		Op:       "insert",
		Table:    "Bridge",
		Row:      bridge,
		UUIDName: namedBridgeUuid,
	}
	// Inserting a Bridge row in Bridge table requires mutating the open_vswitch table.
	mutateUuid := []libovsdb.UUID{libovsdb.UUID{namedBridgeUuid}}
	mutateSet, _ := libovsdb.NewOvsSet(mutateUuid)
	mutation := libovsdb.NewMutation("bridges", "insert", mutateSet)
	/*	log.Println()
		log.Println("+++++++++++++++", getRootUuid())*/

	condition := libovsdb.NewCondition("_uuid", "==", libovsdb.UUID{getRootUuid()})

	// simple mutate operation
	mutateOp := libovsdb.Operation{
		Op:        "mutate",
		Table:     "Open_vSwitch",
		Mutations: []interface{}{mutation},
		Where:     []interface{}{condition},
	}

	operations := []libovsdb.Operation{insertIntfOp, insertPortOp, insertBridgeOp, mutateOp}
	reply, _ := ovsClient.Transact("Open_vSwitch", operations...)

	if len(reply) < len(operations) {
		return "", errors.New("Number of Replies should be atleast equal to number of Operations")
	}
	for i, o := range reply {
		if o.Error != "" && i < len(operations) {
			return "", errors.New("Transaction Failed due to an error :" + o.Error + " details : " + o.Details)
		} else if o.Error != "" {
			return "", errors.New("Transaction Failed due to an error :" + o.Error + " details : " + o.Details)
		}
	}
	return reply[0].UUID.GoUuid, nil
}

func DeleteOVSBridge(ovsClient *libovsdb.OvsdbClient, bridgeName, bridgeUuid string) error {
	condition := libovsdb.NewCondition("name", "==", bridgeName)

	deleteOp := libovsdb.Operation{
		Op:    "delete",
		Table: "Bridge",
		Where: []interface{}{condition},
	}

	// Deleting a Bridge row in Bridge table requires mutating the open_vswitch table.
	mutateUuid := []libovsdb.UUID{libovsdb.UUID{bridgeUuid}}
	mutateSet, _ := libovsdb.NewOvsSet(mutateUuid)
	mutation := libovsdb.NewMutation("bridges", "delete", mutateSet)
	// hacked Condition till we get Monitor / Select working
	log.Println("++++++++++++++", getRootUuid())
	condition = libovsdb.NewCondition("_uuid", "==", libovsdb.UUID{getRootUuid()})

	// simple mutate operation
	mutateOp := libovsdb.Operation{
		Op:        "mutate",
		Table:     "Open_vSwitch",
		Mutations: []interface{}{mutation},
		Where:     []interface{}{condition},
	}

	operations := []libovsdb.Operation{deleteOp, mutateOp}
	reply, _ := ovsClient.Transact("Open_vSwitch", operations...)

	if len(reply) < len(operations) {
		return errors.New("Number of Replies should be at least equal to number of Operations")
	}

	for i, o := range reply {
		if o.Error != "" && i < len(operations) {
			return errors.New("Transaction Failed due to an error : " + o.Error + " details : " + o.Details)
		} else if o.Error != "" {
			return errors.New("Transaction Failed due to an error : " + o.Error + " details : " + o.Details)
		}
	}
	return nil
}

func getRootUuid() string {
	for uuid, _ := range cache["Open_vSwitch"] {
		return uuid
	}
	return ""
}

func addVxlanPort(ovsClient *libovsdb.OvsdbClient, bridgeName string, portName string, peerAddress string) {
	namedPortUuid := "port"
	namedIntfUuid := "intf"

	options := make(map[string]interface{})
	options["remote_ip"] = peerAddress
	// intf row to insert
	intf := make(map[string]interface{})
	intf["name"] = portName
	intf["type"] = `vxlan`
	intf["options"], _ = libovsdb.NewOvsMap(options)

	insertIntfOp := libovsdb.Operation{
		Op:       "insert",
		Table:    "Interface",
		Row:      intf,
		UUIDName: namedIntfUuid,
	}

	// port row to insert
	port := make(map[string]interface{})
	port["name"] = portName
	port["interfaces"] = libovsdb.UUID{namedIntfUuid}

	insertPortOp := libovsdb.Operation{
		Op:       "insert",
		Table:    "Port",
		Row:      port,
		UUIDName: namedPortUuid,
	}

	// Inserting a row in Port table requires mutating the bridge table.
	mutateUuid := []libovsdb.UUID{libovsdb.UUID{namedPortUuid}}
	mutateSet, _ := libovsdb.NewOvsSet(mutateUuid)
	mutation := libovsdb.NewMutation("ports", "insert", mutateSet)
	condition := libovsdb.NewCondition("name", "==", bridgeName)

	// simple mutate operation
	mutateOp := libovsdb.Operation{
		Op:        "mutate",
		Table:     "Bridge",
		Mutations: []interface{}{mutation},
		Where:     []interface{}{condition},
	}
	operations := []libovsdb.Operation{insertIntfOp, insertPortOp, mutateOp}
	reply, _ := ovsClient.Transact("Open_vSwitch", operations...)
	if len(reply) < len(operations) {
		log.Println("Number of Replies should be atleast equal to number of Operations")
	}
	for i, o := range reply {
		if o.Error != "" && i < len(operations) {
			log.Println("Transaction Failed due to an error :", o.Error, " details:", o.Details, " in ", operations[i])
		} else if o.Error != "" {
			log.Println("Transaction Failed due to an error :", o.Error)
		}
	}
}

func portUuidForName(portName string) string {
	portCache := cache["Port"]
	for key, val := range portCache {
		if val.Fields["name"] == portName {
			return key
		}
	}
	return ""
}

func portExists(ovsClient *libovsdb.OvsdbClient, portName string) (bool, error) {
	condition := libovsdb.NewCondition("name", "==", portName)
	selectOp := libovsdb.Operation{
		Op:    "select",
		Table: "Port",
		Where: []interface{}{condition},
	}
	operations := []libovsdb.Operation{selectOp}
	reply, _ := ovsClient.Transact("Open_vSwitch", operations...)

	if len(reply) < len(operations) {
		return false, errors.New("Number of Replies should be atleast equal to number of Operations")
	}

	if reply[0].Error != "" {
		errMsg := fmt.Sprintf("Transaction Failed due to an error: %v", reply[0].Error)
		return false, errors.New(errMsg)
	}

	if len(reply[0].Rows) == 0 {
		return false, nil
	}
	return true, nil
}

func deletePort(ovsClient *libovsdb.OvsdbClient, bridgeName string, portName string) {
	condition := libovsdb.NewCondition("name", "==", portName)
	deleteOp := libovsdb.Operation{
		Op:    "delete",
		Table: "Port",
		Where: []interface{}{condition},
	}

	portUuid := portUuidForName(portName)
	if portUuid == "" {
		// glog.Error("Unable to find a matching Port : ", portName)
		return
	}
	// Deleting a Bridge row in Bridge table requires mutating the open_vswitch table.
	mutateUuid := []libovsdb.UUID{libovsdb.UUID{portUuid}}
	mutateSet, _ := libovsdb.NewOvsSet(mutateUuid)
	mutation := libovsdb.NewMutation("ports", "delete", mutateSet)
	condition = libovsdb.NewCondition("name", "==", bridgeName)

	// simple mutate operation
	mutateOp := libovsdb.Operation{
		Op:        "mutate",
		Table:     "Bridge",
		Mutations: []interface{}{mutation},
		Where:     []interface{}{condition},
	}

	operations := []libovsdb.Operation{deleteOp, mutateOp}
	reply, _ := ovsClient.Transact("Open_vSwitch", operations...)

	if len(reply) < len(operations) {
		// glog.Error("Number of Replies should be atleast equal to number of Operations")
	}
	for i, o := range reply {
		if o.Error != "" && i < len(operations) {
			// glog.Error("Transaction Failed due to an error :", o.Error, " in ", operations[i])
		} else if o.Error != "" {
			// glog.Error("Transaction Failed due to an error :", o.Error)
		}
	}
}

func UpdatePortContext(ovsClient *libovsdb.OvsdbClient, portName string, key string, context string) error {
	config := make(map[string]string)
	config[CONTEXT_KEY] = key
	config[CONTEXT_VALUE] = context
	other_config, _ := libovsdb.NewOvsMap(config)

	mutation := libovsdb.NewMutation("other_config", "insert", other_config)
	condition := libovsdb.NewCondition("name", "==", portName)

	// simple mutate operation
	mutateOp := libovsdb.Operation{
		Op:        "mutate",
		Table:     "Interface",
		Mutations: []interface{}{mutation},
		Where:     []interface{}{condition},
	}

	operations := []libovsdb.Operation{mutateOp}
	reply, _ := ovsClient.Transact("Open_vSwitch", operations...)
	if len(reply) < len(operations) {
		return errors.New("Number of Replies should be atleast equal to number of Operations")
	}
	for i, o := range reply {
		if o.Error != "" && i < len(operations) {
			return errors.New(fmt.Sprintln("Transaction Failed due to an error :", o.Error, " details:", o.Details, " in ", operations[i]))
		} else if o.Error != "" {
			return errors.New(fmt.Sprintln("Transaction Failed due to an error :", o.Error))
		}
	}
	return nil
}

func AddInternalPort(ovsClient *libovsdb.OvsdbClient, bridgeName string, portName string, tag uint) error {
	namedPortUuid := "port"
	namedIntfUuid := "intf"

	// intf row to insert
	intf := make(map[string]interface{})
	intf["name"] = portName
	intf["type"] = `internal`

	insertIntfOp := libovsdb.Operation{
		Op:       "insert",
		Table:    "Interface",
		Row:      intf,
		UUIDName: namedIntfUuid,
	}

	// port row to insert
	port := make(map[string]interface{})
	port["name"] = portName
	port["interfaces"] = libovsdb.UUID{namedIntfUuid}

	if tag != 0 {
		port["tag"] = tag
	}

	insertPortOp := libovsdb.Operation{
		Op:       "insert",
		Table:    "Port",
		Row:      port,
		UUIDName: namedPortUuid,
	}

	// Inserting a row in Port table requires mutating the bridge table.
	mutateUuid := []libovsdb.UUID{libovsdb.UUID{namedPortUuid}}
	mutateSet, _ := libovsdb.NewOvsSet(mutateUuid)
	mutation := libovsdb.NewMutation("ports", "insert", mutateSet)
	condition := libovsdb.NewCondition("name", "==", bridgeName)

	// simple mutate operation
	mutateOp := libovsdb.Operation{
		Op:        "mutate",
		Table:     "Bridge",
		Mutations: []interface{}{mutation},
		Where:     []interface{}{condition},
	}

	operations := []libovsdb.Operation{insertIntfOp, insertPortOp, mutateOp}
	reply, _ := ovsClient.Transact("Open_vSwitch", operations...)
	if len(reply) < len(operations) {
		// glog.Error("Number of Replies should be atleast equal to number of Operations")
		return errors.New("Number of Replies should be atleast equal to number of Operations")
	}
	for i, o := range reply {
		if o.Error != "" && i < len(operations) {
			msg := fmt.Sprintf("Transaction Failed due to an error : %v details: %v in %v", o.Error, o.Details, operations[i])
			return errors.New(msg)
		} else if o.Error != "" {
			msg := fmt.Sprintf("Transaction Failed due to an error : %v", o.Error)
			return errors.New(msg)
		}
	}
	return nil
}

func populateCache(updates libovsdb.TableUpdates) {
	for table, tableUpdate := range updates.Updates {
		if _, ok := cache[table]; !ok {
			cache[table] = make(map[string]libovsdb.Row)

		}
		for uuid, row := range tableUpdate.Rows {
			empty := libovsdb.Row{}
			if !reflect.DeepEqual(row.New, empty) {
				cache[table][uuid] = row.New
			} else {
				delete(cache[table], uuid)
			}
		}
	}
}

func ovs_connect() (*libovsdb.OvsdbClient, error) {
	quit = make(chan bool)
	update = make(chan *libovsdb.TableUpdates)
	cache = make(map[string]map[string]libovsdb.Row)

	// By default libovsdb connects to 127.0.0.1:6440.
	var ovsClient *libovsdb.OvsdbClient
	var err error
	for {
		ovsClient, err = libovsdb.Connect("", 0)
		if err != nil {
			log.Printf("Error(%s) connecting to OVS. Retrying...\n", err.Error())
			time.Sleep(time.Second * 2)
			continue
		}
		break
	}
	var notifier Notifier
	ovsClient.Register(notifier)

	initial, _ := ovsClient.MonitorAll("Open_vSwitch", "")
	populateCache(*initial)
	go monitorDockerBridge(ovsClient)
	for getRootUuid() == "" {
		time.Sleep(time.Second * 1)
	}
	log.Println("Connected to OVS...")
	return ovsClient, nil
}

type Notifier struct {
}

func (n Notifier) Update(context interface{}, tableUpdates libovsdb.TableUpdates) {
	populateCache(tableUpdates)
	update <- &tableUpdates
}
func (n Notifier) Disconnected(ovsClient *libovsdb.OvsdbClient) {
}
func (n Notifier) Locked([]interface{}) {
}
func (n Notifier) Stolen([]interface{}) {
}
func (n Notifier) Echo([]interface{}) {
}

package main

import (
	"chord/utils"
	"fmt"
	"log"
	"net/rpc"
	"strconv"
)

func main() {
	var service string
	for {
		fmt.Println("Choose the service you want,press:")
		fmt.Println("-->0 to get a resource")
		fmt.Println("-->1 to put a resource")
		fmt.Println("-->2 to make a node leave the ring")
		fmt.Println("-->3 to delete a resource")
		fmt.Scan(&service)
		serviceValue, err := strconv.Atoi(service)
		if err != nil {
			serviceValue = -1
		}
		switch serviceValue {
		case 0:
			get()
			break
		case 1:
			put()
			break
		case 2:
			leave()
			break
		case 3:
			remove()
			break
		default:
			break
		}
	}
}

var address = "0.0.0.0"

func remove() {
	conf, err := utils.ReadJSON("../config.json")
	if err != nil {
		log.Println("Error in reading JSON file:", err.Error())
	}
	print("json read ")
	client, err := rpc.DialHTTP("tcp", address+":"+conf.Port_number)
	fmt.Print("Insert the resource id you want to delete:")
	args := new(utils.PutArgs)
	reply := new(utils.ValueReply)
	fmt.Scan(&args.Id)
	client.Call("ServiceRegistry.Delete", *args, reply)
	if reply.Val == "" {
		fmt.Print("impossible to delete resource")
	} else {
		println("The resource with id", args.Id, "and value", reply.Val, "has been deleted")
	}
}
func get() {
	conf, err := utils.ReadJSON("../config.json")
	if err != nil {
		log.Println("Error in reading JSON file:", err.Error())
	}
	println("json read ")
	client, err := rpc.DialHTTP("tcp", address+":"+conf.Port_number)

	fmt.Print("Insert the resource id you want to get:")
	args := new(utils.Args)
	fmt.Scan(&args.Id)

	reply := new(utils.ValueReply)
	client.Call("ServiceRegistry.Get", *args, reply)
	if reply.Val == "" {
		fmt.Print("impossible to get resource")
	} else {
		println("The resource with id", args.Id, "is:", reply.Val)
	}

}
func put() {
	conf, err := utils.ReadJSON("../config.json")
	if err != nil {
		log.Println("Error in reading JSON file:", err.Error())
	}
	client, err := rpc.DialHTTP("tcp", address+":"+conf.Port_number)
	args := new(utils.PutArgs)
	args.RecursionCounter = 0
	reply := new(utils.ValueReply)
	reply.Id = -1
	fmt.Print("Insert the resource you want to insert:")
	fmt.Scan(&args.Value)

	client.Call("ServiceRegistry.Put", *args, reply)
	if reply.Id == -1 {
		fmt.Print("Impossible to put resource")
		return
	}
	println("The resource", args.Value, "entered with id:", reply.Id)
}
func leave() {
	//chiedi al service registry quali nodi sono attualmenete nel sistema
	conf, err := utils.ReadJSON("../config.json")
	if err != nil {
		log.Println("Error in reading JSON file:", err.Error())
	}
	client, err := rpc.DialHTTP("tcp", address+":"+conf.Port_number)
	if err != nil {
		println("Impossible to contact service registry")
		return
	}
	rep := new(utils.NodesArray)
	client.Call("ServiceRegistry.RetrieveNodes", *new(utils.Args), rep)
	if err != nil {
		println("Impossible to retrieve nodes")
		return
	}

	fmt.Println("Nodes in the ring are:")
	for i := 0; i < len(rep.Ids); i++ {
		fmt.Print(rep.Ids[i], "\n")
	}
	args := new(utils.Args)
	fmt.Print("Insert the node id you want to leave the ring")
	//invoca LeaveRequest sul service registry passandogli il node id
	fmt.Scan(&args.Id)
	reply := new(utils.PutArgs)
	err = client.Call("ServiceRegistry.LeaveRequest", *args, reply)
	if err != nil {
		log.Println("Error in leave request")
		return
	}
	if reply.Value == "" {
		println("No node with this id")
		return
	}

	println("Container arrestato con successo")
	return
}

package main

import (
	"chord/utils"
	"context"
	"crypto/sha1"
	"errors"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"
)

type ServiceRegistry struct {
	ServiceMapping map[int]*utils.IP_PN_Mapping
	ring_size      int
	Bootstrap      int
	done           bool
}

// Aggiungi un nuovo servizio alla mappa
func (sr *ServiceRegistry) AddService(args utils.Address, reply *utils.Reply) error {
	address := new(utils.IP_PN_Mapping)
	address.IP = args.Ip
	address.PN = args.Pn
	address.HostName = args.Name
	sr.ServiceMapping[args.Id] = address
	reply.Reply = true
	return nil
}

// Rimuovi un servizio dalla mappa
func (sr *ServiceRegistry) RemoveService(args utils.Address, reply *utils.Reply) error {
	delete(sr.ServiceMapping, args.Id)
	return nil
}

func (sr *ServiceRegistry) setNewBootstrap() error {
	for key := range sr.ServiceMapping {

		client, err := rpc.DialHTTP("tcp", sr.ServiceMapping[key].HostName+":"+sr.ServiceMapping[key].PN)

		if err != nil {
			break
		} else {
			//se riesco a contattare il nodo significa che è ancora attivo, allora lo imposto come bootstrap
			sr.Bootstrap = key
			client.Close()
			return nil
		}

	}

	return errors.New("No active node in the system to be a bootstrap node!")
}

var sr_mutex sync.Mutex

func firstUpdate(sr *ServiceRegistry) error {
	println("Updating finger tables of the nodes!")
	for key := range sr.ServiceMapping {
		println("calling update on node:", key)

		client, err := rpc.DialHTTP("tcp", sr.ServiceMapping[key].HostName+":"+sr.ServiceMapping[key].PN)
		defer client.Close()
		if err != nil {
			log.Println("Error in connecting to:", sr.ServiceMapping[key].HostName+":"+sr.ServiceMapping[key].PN)

			return err
		}
		nodeId := new(utils.Args)
		replyNode := new(utils.ChordNode)
		err = client.Call("ChordNode.GetNodeInfo", *nodeId, replyNode)
		if err != nil {
			log.Println("RPC error:", err.Error())

			return err
		}

		rep := new(utils.Reply)
		err = client.Call("ChordNode.UpdateFTRequest", *replyNode, rep)

		if err != nil {

			log.Println("RPC error on UpdateFTRequest", err.Error())
			return err
		}

	}
	return nil
}

func (sr *ServiceRegistry) JoinRequest(args utils.IP_PN_Mapping, joinReply *utils.ChordNode) error {
	/*sr_mutex.Lock()
	defer sr_mutex.Unlock()*/
	println("Join request invoked")
	//nodo di bootstrap
	ring_size, err := strconv.Atoi(os.Getenv("RING_SIZE"))
	if err == nil {
		if sr.Bootstrap != -1 {
			println("Ring has alreay some node")
			//attendo attivazione del service registry
			sr.waitForDone()
			bootstrapNode := sr.getBootstrap()
			//contatto il nodo di bootstrap mediante RPC
			println("Trying to contact the bootstrap node")
			client, err := rpc.DialHTTP("tcp", bootstrapNode.HostName+":"+bootstrapNode.PN)
			defer client.Close()

			if err != nil {
				log.Println("Service registry error connecting to the bootstrap node:", err.Error())
				bootstrapError := sr.setNewBootstrap()
				if bootstrapError != nil {
					return bootstrapError
				}
				bootstrapNode := sr.getBootstrap()
				//contatto il nodo di bootstrap mediante RPC
				client, err = rpc.DialHTTP("tcp", bootstrapNode.HostName+":"+bootstrapNode.PN)
				defer client.Close()
			}
			println("Generating chord node id")
			id := sr.generateChordNodeID(args.IP, args.PN, ring_size)
			reply := new(utils.ChordNode)
			reply.FingerTable = make([]int, utils.CountBits(ring_size))

			nodeArgs := new(utils.ChordNode)
			nodeArgs.Id = id
			nodeArgs.FtSize = utils.CountBits(ring_size)
			nodeArgs.RingSize = ring_size
			nodeArgs.FingerTable = make([]int, nodeArgs.FtSize)
			err = client.Call("ChordNode.Join", *nodeArgs, reply)
			if err != nil {
				log.Println("node registration failed:", err.Error())
				return err
			}
			if reply.Id != -1 {
				log.Println("Node joined the ring with id:", id)
			}
			joinReply.Id = reply.Id
			joinReply.Pred = reply.Pred
			joinReply.Succ = reply.Succ
			joinReply.Data = reply.Data
			joinReply.FtSize = reply.FtSize
			joinReply.FingerTable = make([]int, joinReply.FtSize)
			for i := 0; i < joinReply.FtSize; i++ {
				joinReply.FingerTable[i] = -1
			}
			joinReply.RingSize = reply.RingSize

			//per tutti i nodi registrati nel service registry chiedo di aggiornare la propria FT in seguito all'entrata di un nodo
			client.Close()

			firstUpdate(sr)

		} else {

			//se ancora non c'è nessun nodo nella rete il service registry
			//fa entrare direttamente il nodo nell'anello
			addr := new(utils.Address)
			id := sr.generateChordNodeID(args.IP, args.PN, ring_size)
			addr.Ip = args.IP
			addr.Pn = args.PN
			addr.Id = id
			addr.Name = args.HostName
			sr.AddService(*addr, new(utils.Reply))
			sr.done = true
			joinReply.RingSize = ring_size
			joinReply.Id = id
			joinReply.Succ = id
			joinReply.Pred = id
			joinReply.Data = nil
			joinReply.FtSize = utils.CountBits(ring_size)
			joinReply.FingerTable = make([]int, joinReply.FtSize)
			for i := 1; i < joinReply.FtSize; i++ {

				joinReply.FingerTable[i-1] = joinReply.Id
			}
			if addr.Id == -1 {
				log.Println("Node registration failed")

				return nil
			}

		}
	} else {
		log.Println("Error in string conversion:", err)
		joinReply.Id = -1
		return nil
	}

	return nil
}
func (sr *ServiceRegistry) waitForDone() {

	for !sr.done {
		time.Sleep(time.Millisecond * 300)
	}

}
func (sr *ServiceRegistry) ActivateBootstrap(args utils.Address, reply *utils.Reply) error {

	sr.AddService(args, reply)

	sr.Bootstrap = args.Id
	reply.Reply = true
	return nil
}
func (sr *ServiceRegistry) LeaveRequest(args utils.Args, leaveReply *utils.PutArgs) error {

	//vedo se il nodo esiste
	for key := range sr.ServiceMapping {
		if key == args.Id {
			//contatta nodo per chiedergli di uscire dall'anello
			client, err := sr.contactNode(args.Id)
			defer client.Close()

			print("Trying to contact node", args.Id, "\n")
			if err != nil {
				log.Println("error in contacting the node", err.Error())
				return err
			}
			rep := new(utils.Reply)
			arg := new(utils.ChordNode)
			client.Call("ChordNode.Leave", *arg, rep)
			if !rep.Reply {
				return errors.New("Error in node leave")
			}
			println("leave request correctly performed")
			leaveReply.Value = sr.ServiceMapping[args.Id].HostName
			client.Call("ChordNode.Off", *arg, rep)
			delete(sr.ServiceMapping, args.Id)
			sr.setNewBootstrap()
			//update finger table degli altri nodi
			for key := range sr.ServiceMapping {
				client, err := rpc.DialHTTP("tcp", sr.ServiceMapping[key].HostName+":"+sr.ServiceMapping[key].PN)

				if err != nil {
					log.Println("error connecting to the node", err.Error())
					return err
				}
				args := new(utils.Args)
				args.Id = key
				rep := new(utils.Reply)
				client.Call("ChordNode.UpdateFTRequest", *args, rep)
				client.Close()
				client, err = rpc.DialHTTP("tcp", sr.ServiceMapping[key].HostName+":"+sr.ServiceMapping[key].PN)

				err = client.Call("ChordNode.NewPredecessor", *args, rep)
				if err != nil {
					log.Println("Error in updating predecessor", err.Error())
					return err
				}
				client.Close()
			}
			return nil

		}
	}
	leaveReply.Value = ""
	return nil

}
func (sr *ServiceRegistry) RetrieveBootstrap(args utils.Args, reply *utils.Reply) error {

	if sr.Bootstrap != -1 {
		reply.Reply = true
	} else {
		reply.Reply = false
	}
	return nil
}
func (sr *ServiceRegistry) getBootstrap() *utils.IP_PN_Mapping {
	return sr.ServiceMapping[sr.Bootstrap]
}
func (sr *ServiceRegistry) checkNode(id int) bool {
	for key := range sr.ServiceMapping {
		if key == id {
			return true
		}
	}
	return false
}
func newServiceRegistry() *ServiceRegistry {
	rs, err := strconv.Atoi(os.Getenv("RING_SIZE"))
	if err != nil {
		log.Fatal("error in string conversion to int")
	}
	return &ServiceRegistry{
		ServiceMapping: make(map[int]*utils.IP_PN_Mapping),
		ring_size:      rs,
		Bootstrap:      -1,
	}

}

func main() {
	serviceRegistry := newServiceRegistry()
	rpc.Register(serviceRegistry)
	rpc.HandleHTTP()
	listener, err := net.Listen("tcp", ":"+os.Getenv("PORT"))
	if err != nil {
		log.Fatal("Error starting service registry:", err)
	}
	go updateFingerTable(serviceRegistry)
	http.Serve(listener, nil)
}
func (sr *ServiceRegistry) UpdateNeighbours(args utils.Args, rep *utils.Reply) error {
	println("entered into updateneighbours")
	keys := make([]int, 0, len(sr.ServiceMapping))

	// Iterate over the map and collect the keys
	for key := range sr.ServiceMapping {
		keys = append(keys, key)
	}
	var pred int
	var succ int
	sort.Sort(sort.IntSlice(keys))
	for i := 0; i < len(keys); i++ {
		if keys[i] == args.Id {
			pred = keys[i-1]
			succ = keys[i+1]
			delete(sr.ServiceMapping, i)
			break
		}
	}
	argsSucc := new(utils.Args)
	argsSucc.Id = succ
	argsPred := new(utils.Args)
	argsPred.Id = pred
	r := new(utils.Reply)
	client, _ := rpc.DialHTTP("tcp", sr.ServiceMapping[succ].HostName+":"+sr.ServiceMapping[succ].PN)

	client.Call("ChordNode.UpdatePredecessor", *argsPred, r)
	client.Close()
	client, _ = rpc.DialHTTP("tcp", sr.ServiceMapping[pred].HostName+":"+sr.ServiceMapping[pred].PN)

	client.Call("ChordNode.UpdateSuccessor", *argsSucc, r)
	client.Close()
	return nil
}

var update_mutex sync.Mutex

func updateFingerTable(sr *ServiceRegistry) {
	time.Sleep(time.Minute * 2)
	for {

		for key := range sr.ServiceMapping {
			println("Trying to connect to node", key)
			client, err := rpc.DialHTTP("tcp", sr.ServiceMapping[key].HostName+":"+sr.ServiceMapping[key].PN)

			if err != nil {
				//log.Fatal("Error connecting to the node:", err)
				log.Println("Error in connecting to the node", err.Error())
				//aggiorno predecessore del successore ed il successore del predecessore del nodo che è uscito
				args := new(utils.Args)
				rep := new(utils.Reply)
				args.Id = key
				sr.UpdateNeighbours(*args, rep)
				delete(sr.ServiceMapping, key)
				for key := range sr.ServiceMapping {
					client, err = rpc.DialHTTP("tcp", sr.ServiceMapping[key].HostName+":"+sr.ServiceMapping[key].PN)

					if err != nil {
						log.Println("error connecting to the node", err.Error())
						return
					}
					args := new(utils.Args)
					args.Id = key
					rep := new(utils.Reply)
					client.Call("ChordNode.UpdateFTRequest", *args, rep)
					client.Close()
					client, err = rpc.DialHTTP("tcp", sr.ServiceMapping[key].HostName+":"+sr.ServiceMapping[key].PN)

					err = client.Call("ChordNode.NewPredecessor", *args, rep)
					if err != nil {
						log.Println("Error in updating predecessor", err.Error())
						return
					}
					client.Close()
				}
				break
			}
			time.Sleep(time.Second * 10)
			/*	time.Sleep(time.Minute * 1)
				for key := range sr.ServiceMapping {
					println("calling update on node:", key)

					client, err = rpc.DialHTTP("tcp", sr.ServiceMapping[key].HostName+":"+sr.ServiceMapping[key].PN)
					defer client.Close()
					if err != nil {
						log.Println("Error in connecting to:", sr.ServiceMapping[key].HostName+":"+sr.ServiceMapping[key].PN)

						break
					}
					nodeId := new(utils.Args)
					replyNode := new(utils.ChordNode)
					err = client.Call("ChordNode.GetNodeInfo", *nodeId, replyNode)
					if err != nil {
						log.Println("RPC error:", err.Error())
						break
					}

					rep := new(utils.Reply)
					err = client.Call("ChordNode.UpdateFTRequest", *replyNode, rep)

					if err != nil {

						log.Println("RPC error on UpdateFTRequest", err.Error())
						break
					}

				}*/
		}

	}
}

func (sr *ServiceRegistry) generateChordNodeID(input string, input2 string, ringSize int) int {
	data := input + input2
	hash := sha1.Sum([]byte(data)) // Calcola l'hash SHA-1 dei dati
	// Converti l'hash in un numero intero (big.Int)
	hashInt := new(big.Int)
	hashInt.SetBytes(hash[:])
	// Effettua la mappatura nel range [0, ringSize-1]
	nodeID := new(big.Int).Mod(hashInt, big.NewInt(int64(ringSize)))
	return sr.checkId(int(nodeID.Int64()))
}

func (sr ServiceRegistry) Delete(args utils.PutArgs, reply *utils.ValueReply) error {
	client, err := sr.contactBootstrap()
	//invio richiesta get al nodo bootstrap
	timeout := 30 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel() // Assicurati di cancellare il contesto alla fine della funzione
	closeCtx := make(chan struct{})
	go func() {
		err = client.Call("ChordNode.Delete", args, reply)
		defer client.Close()

		if err != nil {
			log.Println("Error:", err.Error())
			reply.Val = ""

		}
		close(closeCtx)
		return

	}()
	select {
	case <-closeCtx:
		return nil
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			log.Println("Timeout: la chiamata RPC ha impiegato troppo tempo.")
			return nil
		}
	}

	return nil

}
func (sr ServiceRegistry) Get(args utils.Args, reply *utils.ValueReply) error {
	println("Service registry invoked")
	client, err := sr.contactBootstrap()
	//invio richiesta get al nodo bootstrap
	timeout := 10 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel() // Assicurati di cancellare il contesto alla fine della funzione
	closeCtx := make(chan struct{})
	go func() {
		err = client.Call("ChordNode.Get", args, reply)

		defer client.Close()
		if err != nil {
			log.Println("Error:", err.Error())
			reply.Val = ""

		}

		close(closeCtx)
		return

	}()
	select {
	case <-closeCtx:
		return nil
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			log.Println("Timeout: la chiamata RPC ha impiegato troppo tempo.")
			return nil
		}
	}
	return nil

}
func (sr ServiceRegistry) Put(args utils.PutArgs, reply *utils.ValueReply) error {
	client, err := sr.contactBootstrap()
	//calcolo id della risorsa
	ring_size, err := strconv.Atoi(os.Getenv("RING_SIZE"))
	if err == nil {
		args.Id = generateResourceID(args.Value, ring_size)
	}
	//invio richiesta put al nodo bootstrap
	timeout := 2 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel() // Assicurati di cancellare il contesto alla fine della funzione
	closeCtx := make(chan struct{})
	go func() {

		err = client.Call("ChordNode.Put", args, reply)
		defer client.Close()

		if err != nil {
			log.Println("Error:", err.Error())
			reply.Id = -1

		}

		println("Id chosen:", reply.Id)
		close(closeCtx)

		return

	}()
	select {
	case <-closeCtx:
		println("line 528 id chosen:", reply.Id)
		return nil

	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			reply.Id = -1
			log.Println("Timeout: la chiamata RPC ha impiegato troppo tempo.")
			return nil
		}
	}

	return nil
}

func (sr ServiceRegistry) contactNode(id int) (*rpc.Client, error) {
	print("Contacting node with hostname:", sr.ServiceMapping[id].HostName, "and port number:", sr.ServiceMapping[id].PN, "\n")
	client, err := rpc.DialHTTP("tcp", sr.ServiceMapping[id].HostName+":"+sr.ServiceMapping[id].PN)

	if err != nil {
		log.Println("Error connecting to the node:", err)
		return nil, nil
	}
	return client, err
}

func (sr ServiceRegistry) contactBootstrap() (*rpc.Client, error) {
	bootstrapNode := sr.getBootstrap()

	//contatto il nodo di bootstrap mediante RPC

	client, err := rpc.DialHTTP("tcp", bootstrapNode.HostName+":"+bootstrapNode.PN)

	if err != nil {
		log.Println("Error connecting to the bootstrap node:", err)

	}
	return client, err
}
func generateResourceID(input string, size int) int {
	hash := sha1.Sum([]byte(input)) // Calcola l'hash SHA-1 dei dati
	// Converti l'hash in un numero intero (big.Int)
	hashInt := new(big.Int)
	hashInt.SetBytes(hash[:])
	// Effettua la mappatura nel range [0, ringSize-1]
	nodeID := new(big.Int).Mod(hashInt, big.NewInt(int64(size)))
	return int(nodeID.Int64())
}

func (sr *ServiceRegistry) checkId(id int) int {
	// Scorre la sua mappa e vede se c'è un id di qualche entry che corrisponde a id

	counter := 0
	for key := range sr.ServiceMapping {

		if key == id {
			counter++
			if counter < sr.ring_size { // Aggiunto il controllo per evitare ricorsione eccessiva

				return sr.checkId((id + 1) % sr.ring_size)
			} else {

				return -1
			}
		}
	}
	return id
}
func (sr *ServiceRegistry) RetrieveNodes(args utils.Args, reply *utils.NodesArray) error {
	print("Entered in retrieve nodes\n")
	i := 0

	for id, _ := range sr.ServiceMapping {
		reply.Ids = append(reply.Ids, id)
		println("node:", reply.Ids[i])
		i++
	}
	if reply == nil {
		return errors.New("No nodes in the ring")
	}
	return nil
}
func (sr *ServiceRegistry) GetNode(args utils.Args, reply *utils.Address) error {

	for key, value := range sr.ServiceMapping {
		if key == args.Id {
			reply.Pn = value.PN
			reply.Ip = value.IP
			reply.Name = value.HostName
			return nil
		}
	}
	return errors.New("Node not found")
}

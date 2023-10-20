package main

import (
	"chord/utils"
	"context"
	"errors"
	"log"
	"math"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"sync"
	"time"
)

type NodeArgs struct {
	node *ChordNode
}

var address = "register"
var node_mutex sync.Mutex
var done = make(chan (bool))
var maxRetries = 3

func main() {
	println("Node is running")
	node := newNode()
	name, err := os.Hostname()
	mapping := utils.IP_PN_Mapping{
		node.Ip,
		node.Pn,
		name,
	}
	//richiesta al service registry di entrare a far parte dell'anello Chord
	//leggo file config in cui ci sono PN e IP del service registry

	conf, err := utils.ReadJSON("config.json")
	if err != nil {
		log.Println("Error reading configuration file", err)
		return
	}
	println("Trying to contact service registry")
	client, err := rpc.DialHTTP("tcp", address+":"+conf.Port_number)
	defer client.Close()

	if err != nil {
		return
	}

	reply := new(utils.ChordNode)
	reply.Name = name
	println("Invoking join request")
	timeout := 3 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel() // Assicurati di cancellare il contesto alla fine della funzione

	go func() {
		err = client.Call("ServiceRegistry.JoinRequest", mapping, reply)
		if err != nil {

			return

		}
		cancel()

	}()
	select {
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			log.Println("Timeout: la chiamata RPC ha impiegato troppo tempo.")
			return
		}
	}
	println("Creating the chodnode")

	//una volta che il server è un nodo dell'anello lo metto in ascolto
	newChordNode := new(ChordNode)

	newChordNode.Name = name

	newChordNode.Id = reply.Id
	newChordNode.Pred = reply.Pred
	newChordNode.Succ = reply.Succ
	newChordNode.FtSize = reply.FtSize
	newChordNode.ring_size = reply.RingSize
	newChordNode.FingerTable = make([]int, newChordNode.FtSize)

	newChordNode.Data = reply.Data
	for i := 0; i < newChordNode.FtSize; i++ {
		newChordNode.FingerTable[i] = reply.FingerTable[i]
	}
	rpc.Register(newChordNode)
	rpc.HandleHTTP()
	listener, err := net.Listen("tcp", ":"+os.Getenv("NODE_PORT"))
	if err != nil {
		log.Println("Error starting node:", err.Error())
		return
	}
	go func() {
		done <- true
		http.Serve(listener, nil)
	}()
	<-done
	args := new(utils.Args)
	bootReply := new(utils.Reply)
	err = client.Call("ServiceRegistry.RetrieveBootstrap", *args, bootReply)
	if err != nil {
		log.Println("RPC error at line 103:", err.Error())
		return
	}
	if !bootReply.Reply {
		rep := activateBootstrap(client, newChordNode)

		if !rep.Reply {
			log.Println("Impossible to activate bootstrap node")
			return

		}
	} else {
		add := new(utils.Address)
		rep := new(utils.Reply)
		add.Id = newChordNode.Id
		add.Ip = os.Getenv("NODE_IP")
		add.Pn = os.Getenv("NODE_PORT")
		add.Name, _ = os.Hostname()

		err = client.Call("ServiceRegistry.AddService", *add, rep)
		if err != nil {
			log.Println("RPC ERROR at line 124:", err.Error())
			return
		}
		if !rep.Reply {
			log.Println("Error in AddService")
			return
		}
	}

	newChordNode.FingerTable = make([]int, newChordNode.FtSize)
	newChordNode.UpdateFingerTable(*new(utils.ChordNode), new(utils.Reply))

	go PrintFT(newChordNode)

	select {
	case off := <-done:
		if !off {
			return
		}

	}
}
func (node *ChordNode) Off(arg *utils.ChordNode, rep *utils.Reply) error {
	done <- false
	return nil
}

func activateBootstrap(client *rpc.Client, node *ChordNode) *utils.Reply {
	args := new(utils.Address)
	bootReply := new(utils.Reply)

	args.Id = node.Id
	args.Name = node.Name
	args.Pn = os.Getenv("NODE_PORT")
	args.Ip = os.Getenv("NODE_IP")
	err := client.Call("ServiceRegistry.ActivateBootstrap", args, bootReply)
	if err != nil {
		log.Println("RPC ERROR LINE 156", err.Error())
		bootReply.Reply = false

	}
	return bootReply
}
func newNode() *Node {
	return (&Node{os.Getenv("NODE_IP"), os.Getenv("NODE_PORT"), nil})
}

type Node struct {
	Ip        string
	Pn        string
	chordNode *ChordNode
}
type ChordNode struct {
	Name        string
	Id          int
	Pred        int
	Succ        int
	Data        map[int]string
	FtSize      int
	FingerTable []int
	ring_size   int
}

/*
	Algoritmo di join:

1.	Trovare succ(p+1)
2.	succ(p)=succ(p+1)
3.	pred(succ(p))=p
4. succ(pred(p))=p
4.	init FT: for i in [2,m] succ(p+2^i)
5.	predecessore deve aggiornare la propria FT
6.	trasferimento chiavi in [p,succ(p)) da succ(p) a se stesso
*/

func (node *ChordNode) Join(args utils.ChordNode, joinReply *utils.ChordNode) error {
	node_mutex.Lock()
	defer node_mutex.Unlock()
	p := new(ChordNode)
	p.FtSize = args.FtSize
	p.FingerTable = make([]int, args.FtSize)
	p.ring_size = args.RingSize
	p.Id = args.Id

	//sul nodo  cui ho chiesto Join (bootstrap) chiedo di trovare il successore del nuovo nodo che vuole entrare

	succArgs := new(utils.ArgsSuccessor)
	succArgs.Id = p.Id

	rep := new(utils.GetReply)
	err := node.FindSuccessor(*succArgs, rep)
	if err == nil {
		p.Succ = rep.Value

	} else {
		log.Println("error at line 227", err.Error())
		return nil
	}

	if p.Succ != node.Id {
		//chiamata RPC
		client, err := p.callNode(p.Succ)
		if err != nil {
			return err
		}
		defer client.Close()

		if err != nil {
			log.Println("Error connecting to the node", err.Error())
			return err
		}
		//gli chiede il suo predecessore
		args := new(utils.Args)
		rep := new(utils.GetReply)

		err = client.Call("ChordNode.GetPredecessor", *args, rep)
		if err != nil {
			log.Println("line 230 RPC error", err.Error())
			return err
		}
		//setta il suo nuovo predecessore
		p.Pred = rep.Value
		p.Data = make(map[int]string)
		//set pred(succ(p))=p
		replySucc := new(utils.Reply)
		argsSucc := new(utils.Args)
		argsSucc.Id = p.Id
		//update pred(succ(p))=p

		err = client.Call("ChordNode.UpdatePredecessor", *argsSucc, replySucc)
		if err != nil {
			log.Fatal("line 243 RPC error:", err.Error())
			return err
		}
	} else {
		//imposto il predecessore di p pari a quello del nodo bootstrap
		p.Pred = node.Pred
		//se il successore è il nodo di bootstrap stesso set del predecessore diretto
		node.Pred = p.Id
	}

	//chiamo il predecessore e gli dico di aggiornare il suo successore
	if p.Pred != node.Id {

		client, err := p.callNode(p.Pred)
		if err != nil {
			return err
		}
		defer client.Close()
		if err != nil {
			return err
		}
		replySucc := new(utils.Reply)
		argsSucc := new(utils.Args)
		argsSucc.Id = p.Id

		err = client.Call("ChordNode.UpdateSuccessor", *argsSucc, replySucc)
		if err != nil {
			log.Println("RPC ERROR LINE 265", err.Error())
		}

	} else {
		node.Succ = p.Id
	}

	dataReply := new(utils.DataReply)
	//chiamo la funzione getData applicata al successore per ottenere i dati con p.Id<=id<succ(p).Id

	getArgs := new(utils.ChordNode)
	getArgs.Id = p.Id
	getArgs.Pred = p.Pred
	getArgs.Succ = p.Succ
	dataReply.Data = make(map[int]string)
	if rep.Value != node.Id {
		//chiamata RPC
		client, err := p.callNode(p.Succ)
		if err != nil {
			return err
		}
		defer client.Close()
		if err != nil {
			log.Println("Error connecting to the node", err.Error())
			return err
		}

		err = client.Call("ChordNode.GetData", *getArgs, dataReply)
		if err != nil {
			log.Println("LINE 277; RPC error ", err.Error())
			return err
		}
	} else {
		//se il successore è il nodo di bootstrap dà direttamente le chiavi comprese tra p ed il suo predecessore
		node.GetData(*getArgs, dataReply)

	}
	//il nodo in questione inserisce nel suo campo Data ciò che è contenuto in dataReply.Data
	for key, val := range dataReply.Data {
		println(key, ":", val)
		(p.Data)[key] = val
	}
	for _, val := range p.Data {
		println(val)
	}
	//una volta che il nodo ha ricevuto correttamente le risorse che p dovrà gestire succ(p) può cancellarle
	reply := new(utils.DataReply)
	removeArgs := new(ChordNode)

	removeArgs.Id = args.Id
	removeArgs.FtSize = args.FtSize
	if p.Succ != node.Id {
		//chiamata RPC
		client, err := p.callNode(p.Succ)
		if err != nil {
			return err
		}
		defer client.Close()

		if err != nil {
			log.Println("RIGA 319: Error connecting to the node", err.Error())
			return err
		}
		err = client.Call("ChordNode.RemoveData", *removeArgs, reply)
		if err != nil {
			log.Println("LINE 324 RPC ERROR", err.Error())
		}
	} else {
		node.RemoveData(removeArgs, reply)
	}
	joinReply.Id = p.Id
	joinReply.Succ = p.Succ
	joinReply.Pred = p.Pred
	joinReply.FtSize = p.FtSize
	joinReply.Data = make(map[int]string)
	for key, val := range p.Data {
		joinReply.Data[key] = val
	}
	//joinReply.Data = p.Data
	joinReply.RingSize = p.ring_size
	return nil
}

func (node *ChordNode) GetNodeInfo(args utils.Args, reply *ChordNode) error {
	reply.Id = node.Id
	reply.Succ = node.Succ
	reply.Pred = node.Pred
	reply.FtSize = node.FtSize
	reply.FingerTable = make([]int, reply.FtSize)
	for i := 0; i < node.FtSize; i++ {
		reply.FingerTable[i] = node.FingerTable[i]
	}
	reply.ring_size = node.ring_size
	reply.Name = node.Name
	return nil
}

func (node *ChordNode) UpdateFTRequest(args utils.ChordNode, reply *utils.Reply) error {
	if node.FingerTable == nil {
		node.FingerTable = make([]int, node.FtSize)
	}
	node.UpdateFingerTable(*new(utils.ChordNode), new(utils.Reply))
	return nil
}

/*
Algoritmo leave dell'anello:
Il nodo p che vuole uscire dall'anello deve:
 1. Traferire tutte le sue chiavi al suo successore
 2. Comunicare al suo successore il nuovo predecessore: pred(succ(p))=pred(p)
3.Comunicare al suo predecessore il nuovo successore: succ(pred(p))=succ(p)
*/

func (node *ChordNode) Leave(args utils.ChordNode, leaveReply *utils.Reply) error {
	log.Println("Calling leave on this node")
	client, err := node.callNode(node.Succ)
	if err != nil {
		return err
	}
	defer client.Close()

	if err != nil {
		log.Println("Error in connecting to node successor:", node.Succ)
		return err
	}

	dataReply := new(utils.Reply)
	dataNode := new(utils.ChordNode)

	dataNode.Data = make(map[int]string)

	for key, value := range node.Data {

		dataNode.Data[key] = value
		println("Key:", key, "Value:", dataNode.Data[key])
	}

	//trasferimento chiavi a succ(p)
	errClient := client.Call("ChordNode.SetData", *dataNode, dataReply)
	if errClient != nil {
		log.Println("Error in setting data")
		return errClient
	}
	println("Calling update predecessor on node ", node.Succ)
	//comunicazione al successore del nuovo predecessore
	//set pred(succ(p))=pred(p)
	arg := new(utils.Args)
	arg.Id = node.Pred

	err = client.Call("ChordNode.UpdatePredecessor", *arg, dataReply)
	if err != nil {
		log.Println("Error in calling update predecessor on node ", node.Succ)
	}
	if dataReply.Reply == false {
		//se UpdatePredecessor non va a buon fine rispondo false
		leaveReply.Reply = false
		err := errors.New("Error in updating predecessor")
		log.Println(err.Error())
		return nil
	}
	//comunucazione al predecessore del nuovo successore
	argsSucc := new(utils.Args)
	argsSucc.Id = node.Succ

	client, err = node.callNode(node.Pred)
	if err != nil {
		return err
	}
	defer client.Close()

	if err != nil {
		return err
	}
	client.Call("ChordNode.UpdateSuccessor", *argsSucc, dataReply)
	if dataReply.Reply == false {
		//se UpdateSuccessor non va a buon fine rispondo false
		leaveReply.Reply = false
		err := errors.New("Error in updating succecessor")
		log.Println(err.Error())
		return nil
	}
	leaveReply.Reply = true
	return nil
}

func (node *ChordNode) NewPredecessor(args utils.Args, reply *utils.Reply) error {
	client, err := node.callNode(node.Succ)
	if err != nil {
		return err
	}
	defer client.Close()

	if err != nil {
		log.Println("Impossible to contact node")
		return err
	}

	err = client.Call("ChordNode.UpdatePredecessor", args, reply)
	return err
}

func (node *ChordNode) UpdatePredecessor(args utils.Args, reply *utils.Reply) error {
	println("UpdatePredecessor correctly invoked")
	node.Pred = args.Id
	reply.Reply = true
	return nil
}
func (node *ChordNode) SetData(args utils.ChordNode, reply *utils.Reply) error {
	print("Setting data on node", node.Id, "\n")

	if node.Data == nil {
		println("Creating map for node ", node.Id)
		node.Data = make(map[int]string)
	}
	for key, val := range args.Data {
		node.Data[key] = val
	}

	return nil
}
func (node *ChordNode) UpdateSuccessor(args utils.Args, reply *utils.Reply) error {
	println("Update successor correctly invoked")

	node.Succ = args.Id
	reply.Reply = true
	println("New successor for ", node.Id, "is: ", node.Succ)
	return nil
}
func (node *ChordNode) SetNewFT(args utils.ChordNode, reply *utils.Reply) error {

	for i := 0; i < node.FtSize; i++ {
		node.FingerTable[i] = args.FingerTable[i]

	}
	reply.Reply = true
	return nil
}
func PrintFT(node *ChordNode) {
	for {
		time.Sleep(10 * time.Second)
		println("Finger table of node:", node.Id)
		for i := 0; i < node.FtSize; i++ {
			println(node.FingerTable[i])
		}
		println("Succ:", node.Succ)
		println("Pred:", node.Pred)
	}
}

var updateMutex sync.Mutex

func (node *ChordNode) UpdateFingerTable(args utils.ChordNode, reply *utils.Reply) error {
	updateMutex.Lock()
	defer updateMutex.Unlock()
	if node.FingerTable == nil || len(node.FingerTable) != node.FtSize {
		node.FingerTable = make([]int, node.FtSize)
	}

	for i := 1; i <= node.FtSize; i++ {

		val := (node.Id + int(math.Pow(2, float64(i-1)))) % node.ring_size
		arg := new(utils.ArgsSuccessor)
		rep := new(utils.GetReply)
		arg.Id = val
		err := node.FindSuccessor(*arg, rep)
		if err != nil {
			log.Println("error at line 493", err.Error())
			return nil
		}
		node.FingerTable[i-1] = rep.Value

	}
	node.Succ = node.FingerTable[0]

	return nil
}

func (node *ChordNode) GetData(args utils.ChordNode, reply *utils.DataReply) error {
	println("GETDATA INVOKED")
	if node.Data == nil {
		return nil
	}
	println("Getting data from node: ", node.Id)
	reply.Data = make(map[int]string)
	for key, val := range node.Data {

		if (args.Pred < args.Id && key <= args.Id /*&& key > args.Pred*/) ||
			(args.Pred > args.Id && (key >= args.Pred || key < args.Id)) {
			println(val)
			reply.Data[key] = val
		}
	}

	return nil
}

func (node *ChordNode) callNode(id int) (rpc.Client, error) {

	//contatto il service registry per ottenere il PN in base all'id
	conf, err := utils.ReadJSON("config.json")
	if err != nil {
		log.Println("Error in reading JSON file:", err.Error())

		return rpc.Client{}, err
	}

	client, err := rpc.DialHTTP("tcp", address+":"+conf.Port_number)
	if err != nil {
		return *(new(rpc.Client)), err
	}
	defer client.Close()
	reply := new(utils.Address)
	args := new(utils.Args)
	args.Id = id
	err = client.Call("ServiceRegistry.GetNode", *args, reply)
	if err != nil {
		log.Println("RPC error at line 518", err.Error())
		return rpc.Client{}, err
	}

	clientNode, nodeErr := rpc.DialHTTP("tcp", reply.Name+":"+reply.Pn)

	if nodeErr != nil {
		return *new(rpc.Client), err
	}

	return *clientNode, nil
}

// funzione RPC da invocare sul nodo a cui si stanno "sottraendo" risorse in carico
func (node *ChordNode) RemoveData(args *ChordNode, reply *utils.DataReply) error {
	newMap := make(map[int]string)
	if node.Data == nil {
		return nil
	}
	for key, value := range node.Data {
		if key > args.Id {
			newMap[key] = value
		}
	}
	node.Data = newMap
	return nil
}
func (node *ChordNode) GetSuccessor(args utils.Args, reply *utils.GetReply) error {
	reply.Value = node.Succ
	return nil
}

func (node *ChordNode) FindSuccessor(args utils.ArgsSuccessor, reply *utils.GetReply) error {

	//se c'è un solo nodo nella rete esso è per forza il successore di args.Id

	if node.Succ == node.Id {
		println("If a riga 579")
		println("Successor for ", args.Id, "is:", node.Succ)
		reply.Value = node.Id
		return nil
	}
	if node.Pred == args.Id {
		println("If a riga 585")
		println("Successor for ", args.Id, "is:", node.Pred)
		reply.Value = node.Pred
		return nil
	}

	if (node.Pred < node.Id && args.Id <= node.Id && args.Id > node.Pred) ||
		(node.Pred > node.Id && (args.Id > node.Pred || args.Id <= node.Id)) {
		//rientra nella porzione di indici gestita da node
		println("if 646")
		reply.Value = node.Id
		return nil
	}
	//args.Id compreso tra  id del nodo ed id del successore del nodo
	if (node.Succ > node.Id && args.Id > node.Id && args.Id <= node.Succ) ||
		(node.Succ < node.Id && (args.Id > node.Id || args.Id <= node.Succ)) {
		println("if 653")
		reply.Value = node.Succ
		return nil
	}
	//altrimenti scorro la fingertable di node
	min := 10 * node.ring_size
	for i := 0; i < node.FtSize-1; i++ {
		//calcolo il minimo
		min = utils.MinInt(min, node.FingerTable[i])
		if node.FingerTable[i] == args.Id {
			println("if 663")
			reply.Value = node.FingerTable[i]
			return nil
		}
		//scorro la fingertable del nodo per capire quale nodo contattare per cercare il successore di args.Id
		if node.FingerTable[i] < args.Id && (node.FingerTable[i+1] > args.Id || node.FingerTable[i+1] < min) {
			if node.FingerTable[i] != node.Id {
				println("if 670")
				//contatto FT[i]
				println("trying to contact node", node.FingerTable[i])
				client, err := node.callNode(node.FingerTable[i])
				if err != nil {
					return err
				}
				defer client.Close()

				if err != nil {
					log.Println("line 598 error in RPC call to the node", node.FingerTable[i])
					return err
				}
				rep := new(utils.GetReply)
				err = client.Call("ChordNode.FindSuccessor", args, rep)
				if err != nil {
					log.Println("line 605 error in calling chordnode", err.Error())
					return err
				}
				reply.Value = rep.Value
				return err
			} else {
				println("if 693")

				reply.Value = node.FingerTable[i]
				return nil
			}
		}

	}
	//se ho scorso tutta la fingertable e non c'è alcuna entry maggiore di args.Id chiamo il nodo più lontano da node, quindi ultima entry della FT
	if node.FingerTable[node.FtSize-1] != node.Id && node.FingerTable[node.FtSize-1] != -1 {

		println("Trying to contact node", node.FingerTable[node.FtSize-1])
		client, err := node.callNode(node.FingerTable[node.FtSize-1])
		if err != nil {
			return err
		}
		defer client.Close()

		if err != nil {
			log.Println("line 669 error in RPC call to the node", err.Error())
			return err
		}
		rep := new(utils.GetReply)
		err = client.Call("ChordNode.FindSuccessor", args, rep)
		if err != nil {
			log.Println("LINE 660 RPC ERROR", err.Error())
			return err
		}
		reply.Value = rep.Value
		return nil

	} else {
		for i := node.FtSize - 1; i >= 0; i-- {
			if node.FingerTable[i] != -1 {
				reply.Value = node.FingerTable[i]
				return nil
			}
		}
	}
	return errors.New("Cannot find successor")
}

func (node *ChordNode) GetPredecessor(args utils.Args, reply *utils.GetReply) error {
	reply.Value = node.Pred
	return nil
}

func (node *ChordNode) insertData(args utils.JoinReply, reply *utils.Reply) error {
	data := args.Node.Data
	nodeData := node.Data
	for key, value := range data {
		nodeData[key] = value
	}
	reply.Reply = true
	return nil
}

/*Algoritmo di get:
Detta k la chiave della risorsa che si sta cercando, bisogna distinguere 3 casi:
	a. Se k appartiene all'iniseme di chiavi di p ritorna direttamente la risorsa
	b. Se p<k<=FTp[1]  inoltra la richiesta al suo successore
	c. Altrimenti se FTp[j]<=k<FTp[j+1] manda la richiesta a q, nodo di indice j
*/

func (node *ChordNode) Get(args utils.Args, reply *utils.ValueReply) error {
	println("Get on node", node.Id, "invoked")

	if (node.Pred < node.Id && args.Id <= node.Id && args.Id > node.Pred) ||
		(node.Pred > node.Id && (args.Id <= node.Id || args.Id > node.Pred)) {
		println("Node", node.Id, "manages the resource with id", args.Id)
		if node.Data == nil {
			println("No data yet")
			reply.Id = args.Id
			reply.Val = ""
			return nil
		}
		if _, pres := node.Data[args.Id]; pres {
			reply.Val = node.Data[args.Id]
			println(reply.Val)
			return nil

		} else {
			reply.Val = ""
			println(reply.Val)
			return nil
		}

	} else {

		//-----------------------------------------------------
		arg := new(utils.ArgsSuccessor)
		arg.Id = args.Id
		rep := new(utils.GetReply)
		//find successor ritorna il node id da contattare
		node.FindSuccessor(*arg, rep)
		println("Successor for ", arg.Id, "is: ", rep.Value)
		//contatto il nodo
		client, err := node.callNode(rep.Value)
		if err != nil {
			return err
		}

		if err != nil {
			log.Println("Error in calling node", err.Error())
			reply.Val = ""
			return nil

		}
		println("Calling Get on node:", rep.Value)
		client.Call("ChordNode.Get", args, reply)
		client.Close()
		return nil
	}

}

var counter int

func checkId(id int, node *ChordNode) int {
	println("Checking id for resource with purposed id:", id)
	if _, pres := node.Data[id]; pres {
		println("The id ", id, " is already in use")
		println("The id of the node is: ", node.Id, "while its successor is: ", node.Succ)
		if node.Id > node.Pred {

			for i := node.Pred + 1; i <= node.Id+1; i++ {
				println("Trying with id:", i)
				if i == node.Id+1 {
					return node.Succ
				}
				if _, pres := node.Data[i]; !pres {
					return i
				}
			}
		} else {

			for j := node.Pred + 1; j <= node.ring_size-1; j++ {
				println("Trying with id:", j)
				if _, pres := node.Data[j]; !pres {
					return j
				}
			}
			for i := 0; i <= node.Id+1; i++ {
				println("Trying with id:", i)
				if i == node.Id+1 {
					return node.Succ
				}
				if _, pres := node.Data[i]; !pres {
					return i
				}
			}
		}
	} else {
		//se id libero lo ritorno
		println("Id chosen is: ", id)
		return id
	}
	println("NON ENTRA IN NESSUN IF ELSE,RIGA 850")
	return node.Succ
}
func (node *ChordNode) Put(args utils.PutArgs, reply *utils.ValueReply) error {
	println("Put on node invoked")
	if args.RecursionCounter == node.ring_size {
		//per limitare la ricorsione quando ho fatto chiamata
		//a put per ring_size volte significa che non ho spazio nell'anello per memorizzare la risorsa
		reply.Id = -1
		return nil
	} else {
		args.RecursionCounter++
	}
	println(node.Pred, "is node's predecessor", node.Id, "is node's id", "and", args.Id, "is the argument")
	if (node.Pred < node.Id && args.Id <= node.Id && args.Id > node.Pred) ||
		(node.Pred > node.Id && (args.Id <= node.Id || args.Id > node.Pred)) {
		println("node ", node.Id, "manages ", args.Id)
		println("Calling checkId on node", node.Id)
		newId := checkId(args.Id, node)
		if node.Data == nil {
			println("Building the map")
			node.Data = make(map[int]string)
		}

		if newId == node.Succ {
			println("Calling successor to put the resource", args.Value, "with Id", newId)
			client, err := node.callNode(node.Succ)
			if err != nil {
				return err
			}
			if err != nil {
				log.Println("Impossible to contact successor")
				reply.Val = args.Value
				reply.Id = -1
				return err
			}
			args.Id = newId

			client.Call("ChordNode.Put", args, reply)
			println("riga 886: The id chosen is:", reply.Id)

			return nil
		}
		println("Id chosen is:", newId)
		println("Node: ", node.Id, "is saving the new data into ", newId)

		node.Data[newId] = args.Value
		println("Value saved into data", node.Data[newId])

		reply.Id = newId
		reply.Val = args.Value
		println("riga 898: the id chosen is:", reply.Id)
		return nil

	} else {
		println("Trying to find successor for", args.Id)
		arg := new(utils.ArgsSuccessor)
		arg.Id = args.Id
		rep := new(utils.GetReply)
		//find successor ritorna il node id da contattare
		node.FindSuccessor(*arg, rep)
		println("Successor for ", arg.Id, "is: ", rep.Value)
		//contatto il nodo
		client, err := node.callNode(rep.Value)
		if err != nil {
			return err
		}
		defer client.Close()

		if err != nil {
			log.Println("Error in calling node", err.Error())
			reply.Val = args.Value
			reply.Id = -1
			return err
		}

		println("Calling Put on node:", rep.Value)

		client.Call("ChordNode.Put", args, reply)
		return nil
	}
}

func (node *ChordNode) Delete(args utils.PutArgs, reply *utils.ValueReply) error {
	if (node.Pred < node.Id && args.Id <= node.Id && args.Id > node.Pred) || (node.Pred > node.Id && (args.Id < node.Id || args.Id > node.Pred)) {
		if node.Data == nil {
			reply.Id = args.Id
			reply.Val = ""
			return errors.New("No resource matching the id")
		}

		reply.Val = node.Data[args.Id]
		delete(node.Data, args.Id)

	} else {
		arg := new(utils.ArgsSuccessor)
		arg.Id = args.Id
		rep := new(utils.GetReply)
		//find successor ritorna il node id da contattare
		node.FindSuccessor(*arg, rep)
		//contatto il nodo
		client, err := node.callNode(rep.Value)
		if err != nil {
			return err
		}
		defer client.Close()

		if err != nil {
			log.Println("Error in calling node", err.Error())
			return err
		}
		client.Call("ChordNode.Delete", args, reply)
	}
	return nil
}

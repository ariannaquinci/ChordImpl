package utils

import (
	"encoding/json"
	"math"
	"os"
)

type ArgsSuccessor struct {
	Id int
}
type Reply struct {
	Reply bool
}
type JoinReply struct {
	Node *ChordNode
}
type Address struct {
	Ip   string
	Pn   string
	Id   int
	Name string
}
type ChordNode struct {
	Name        string
	Id          int
	Pred        int
	Succ        int
	Data        map[int]string
	FtSize      int
	FingerTable []int
	RingSize    int
}

type Config struct {
	Size        int    `json:"ring_size"`
	Ip_address  string `json:"ip_address"`
	Port_number string `json:"port_number"`
}

/*
	type ServiceRegistryMeta struct {
		ServiceMapping map[int]*IP_PN_Mapping
	}
*/
type IP_PN_Mapping struct {
	IP       string
	PN       string
	HostName string
}
type NodesArray struct {
	Ids []int
}
type Args struct {
	Id int
}
type GetReply struct {
	Value int
}
type PutReply struct {
	Reply bool
}
type PutArgs struct {
	Value            string
	Id               int
	RecursionCounter int
}

type DataReply struct {
	Data map[int]string
}

type PNReply struct {
	E  error
	Pn string
}
type ValueReply struct {
	Val string
	Id  int
}

func CountBits(n int) int {
	if n == 0 {
		return 1 // Il numero 0 è rappresentato da un singolo bit
	}
	// Calcola il log2 del valore assoluto di n e aggiungi 1 per ottenere il numero di bit
	res := int(math.Log2(math.Abs(float64(n))))
	return res
}
func MinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func ReadJSON(filename string) (Config, error) {
	var config Config
	configFile, err := os.Open(filename)
	if err != nil {
		return config, err
	}
	defer configFile.Close()
	decoder := json.NewDecoder(configFile)
	err = decoder.Decode(&config)
	if err != nil {
		return config, err
	}
	return config, nil
}

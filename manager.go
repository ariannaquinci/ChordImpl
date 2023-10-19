package main

/*
il manager deve avere le funzionalità per:
	-istanziare un container per il serviceregistry,fargli creare la rete Chord e farlo mettere in ascolto in attesa di richieste
	-istanziare un container per un nodo e attivarlo in modo tale che possa richiedere join all'anello

il main si trova in questo file
*/
import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

type Config struct {
	Size        int    `json:"ring_size"`
	Ip_address  string `json:"ip_address"`
	Port_number string `json:"port_number"`
}
type NodeConfig struct {
	Ip_address  []string `json:"node_ip"`
	Port_number []string `json:"node_pn"`
}
type Node struct {
	Ip string
	Pn string
}

type ServiceRegistry struct {
	Ip       string
	Pn       string
	RingSize int
}

// Template del Dockerfile per il nodo
const dockerfileNodeTemplate = `
# Usa un'immagine di base contenente Go per compilare l'applicazione
FROM golang:1.17 AS build

WORKDIR /app

# Copia i file go.mod per scaricare le dipendenze
COPY go.mod ./
RUN go mod download

# Copia il resto del codice dell'applicazione

COPY server/node.go ./
COPY config.json ./
COPY utils ./utils

# Compila l'applicazione
RUN go build -o node .

# Definisci le variabili d'ambiente per l'indirizzo IP e la porta
ENV NODE_IP $NODE_IP
ENV NODE_PORT $NODE_PORT

EXPOSE $NODE_PORT
# Esegui l'applicazione
CMD ["./node"]
`

// Template del Dockerfile per il service registry
const dockerfileSRTemplate = `
# Usa un'immagine di base contenente Go per compilare l'applicazione
FROM golang:1.17 AS build
WORKDIR /app

# Copia i file go.mod per scaricare le dipendenze
COPY go.mod ./
RUN go mod download

# Copia il resto del codice dell'applicazione
COPY serviceRegistry/serviceRegistry.go ./
COPY utils ./utils

# Compila l'applicazione
RUN go build -o serviceRegistry .
# Definisci le variabili d'ambiente per l'indirizzo IP e la porta
ENV IP $IP
ENV PORT $PORT
ENV RING_SIZE $RING_SIZE

EXPOSE $PORT
# Esegui l'applicazione
CMD ["./serviceRegistry"]
`

func main() {
	// Determina il percorso del file di configurazione
	var configPath string

	path, err := os.Getwd()
	if err != nil {
		fmt.Println("Errore nell'ottenere la working directory:", err)
		return
	}
	configPath = filepath.Join(path, "config.json")

	// Leggi il file di configurazione
	config, err := readJSON(configPath)

	if err != nil {
		fmt.Println("Errore durante la lettura del file di configurazione:", err)
		return
	}

	registry := createServiceRegistry(config)

	//crea file yml per configurazione container service registry con docker compose
	createSRYmlFile(registry)
	//crea dockerfile per il service registry
	createSRDockerFile(registry)

	if err != nil {
		fmt.Println("Errore nell'ottenere la working directory:", err)
		return
	}
	configPath = filepath.Join(path, "nodeConfig.json")

	// Leggi il file di configurazione
	nodeConfig, err := readNodeJSON(configPath)

	if err != nil {
		fmt.Println("Errore durante la lettura del file di configurazione:", err)
		return
	}
	nodes := createNodes(nodeConfig)

	createNodeDockerFile(nodes[0])

	var composeFileContent string
	createNodeYmlFile(nodes, composeFileContent)

}

func readJSON(filename string) (Config, error) {
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
func readNodeJSON(filename string) (NodeConfig, error) {
	var config NodeConfig

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

func createNodes(nodeConfig NodeConfig) []Node {
	var nodeList []Node
	var node Node
	for i := 0; i < len(nodeConfig.Ip_address); i++ {
		node.Ip = nodeConfig.Ip_address[i]
		node.Pn = nodeConfig.Port_number[i]
		nodeList = append(nodeList, node)
	}
	return nodeList
}

func createServiceRegistry(config Config) ServiceRegistry {
	var registry ServiceRegistry

	registry.Ip = config.Ip_address
	registry.Pn = config.Port_number
	registry.RingSize = config.Size
	return registry

}
func createSRDockerFile(serviceRegistry ServiceRegistry) {
	// Carica il template Dockerfile
	tmpl, err := template.New("DockerfileTemplate").Parse(dockerfileSRTemplate)
	if err != nil {
		fmt.Println("Errore durante il caricamento del template:", err)
		return
	}

	// Apri un file Dockerfile per la scrittura
	file, err := os.Create("SRDockerfile")
	if err != nil {
		fmt.Println("Errore durante l'apertura del file Dockerfile:", err)
		return
	}
	defer file.Close()

	err = tmpl.Execute(file, serviceRegistry)
	if err != nil {
		fmt.Println("Errore durante la scrittura del Dockerfile:", err)
	}
}

func createNodeDockerFile(node Node) {
	// Carica il template Dockerfile
	tmpl, err := template.New("DockerfileTemplate").Parse(dockerfileNodeTemplate)
	if err != nil {
		fmt.Println("Errore durante il caricamento del template:", err)
		return
	}

	// Apri un file Dockerfile per la scrittura
	file, err := os.Create("NodeDockerfile")
	if err != nil {
		fmt.Println("Errore durante l'apertura del file Dockerfile:", err)
		return
	}
	defer file.Close()

	// Applica i dati del nodo al template e scrivi nel file Dockerfile
	err = tmpl.Execute(file, node)
	if err != nil {
		fmt.Println("Errore durante la scrittura del Dockerfile:", err)
	}
}

func createNodeYmlFile(nodes []Node, composeFileContent string) error {
	for i, node := range nodes {
		serviceName := fmt.Sprintf("node-%d-app", i) // Aggiungi un prefisso univoco al nome del servizio
		portMap := node.Pn + ":" + node.Pn

		serviceDefinition := fmt.Sprintf(`
  %s:
    hostname: node%d
    build:
      context: .
      dockerfile: "NodeDockerfile"
    ports:
      - "%s"
    environment:
      NODE_IP: "%s"
      NODE_PORT: "%s"
    networks:
      - mynet
    depends_on:
      - service-registry`, serviceName, i, portMap, node.Ip, node.Pn)
		composeFileContent += serviceDefinition
	}

	err := writeToFile("Server.yml", composeFileContent, os.O_APPEND)

	if err != nil {
		fmt.Println("Error:", err)
		return err
	}
	return nil
}

func createSRYmlFile(registry ServiceRegistry) {
	portMap := registry.Pn + ":" + registry.Pn

	composeFileContent := fmt.Sprintf(`version: '3'
networks:
  mynet:
    driver: bridge
services:
  service-registry:
    hostname: register
    build:
      context: .
      dockerfile: "SRDockerfile"
    ports:
      - "%s"
    environment:
      IP: "%s"
      PORT: "%s"
      RING_SIZE: "%d"
    networks:
      - mynet`, portMap,
		registry.Ip,
		registry.Pn, registry.RingSize)

	err := writeToFile("Server.yml", composeFileContent, os.O_CREATE|os.O_TRUNC)

	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println("Server.yml file created successfully!")
}

func writeToFile(filename, content string, mode int) error {
	file, err := os.OpenFile(filename, mode|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString(content)
	if err != nil {
		return err
	}

	return nil
}

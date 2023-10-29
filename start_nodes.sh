#!/bin/bash

# Leggi il file di configurazione per ottenere l'indirizzo IP e la porta del nodo
NODE_CONFIG_FILE="runtimeNode.json"
NODE_IP=$(jq -r '.node_ip' $NODE_CONFIG_FILE)
NODE_PORT=$(jq -r '.node_pn' $NODE_CONFIG_FILE)

# Avvia il container Docker basato sul Dockerfile
sudo docker build -t "runtime-chord-node" -f ./NodeDockerfile .
sudo docker run --rm -p $NODE_PORT:$NODE_PORT --network=chordimpl_mynet -e NODE_PORT=$NODE_PORT "runtime-chord-node"

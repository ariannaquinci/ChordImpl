Per avviare il sistema bisogna preventivamente generare i Dockerfiles.
Ecco i passi neceassari all'avvio del sistema:
1. Compilare manager.go: "go build manager.go"
2. Eseguire il file eseguibile generato dalla compilazione "./manager", questo genererà i dockerfiles a partire dai file json di configurazione ("nodeConfig.json" e "config.json") ed il file "Server.yml"
3. Avviare Docker Compose: "docker-compose -f Server.yml up"
4. Dopo l'avvio del sistema se si vuole aggiungere un nodo si può utilizzare lo script bash: "start_nodes.sh"
5. Per avviare il client:
    a. Aprire un altro terminale e muoversi nella cartella "client"
    b. Compilare client.go: "go build client.go"
    c. Avviare il client: "./client"

Struttura dei files di configrazione:
1. File "nodeConfig.json" riporta due array, uno per gli indirizzi IP e l'altro per i numeri di porta. Il nodo i-esimo ha come ip l'i-esimo elemento del primo array e come port number l'i-esimo elemento del secondo array
2. File "config.json" ha tre campi: "ring_size", che indica la taglia dell'anello, "ip_address" che indica l'ip del service registry e "port_number" che indica il numero di pprta del service registry. 
ATTENZIONE: La taglia dell'anello dev'essere un numero esprimibile come potenza di 2.

La stessa procedura può essere eseguita su una macchina EC2.
Dopo aver creato la macchina ci si può connettere ad essa tramite ssh, specificando la chiave presente nel file ".pem" che si ottiene alla creazione dell'istanza EC2. 
Per ottenere direttamente la stringa con il comando, dopo aver premuto il tasto "start lab" sul sito https://awsacademy.instructure.com/courses/28710/modules/items/2385832 si va a selezionare l'istanza EC2 su cui si vuole eseguire il progetto e si clicca su "client SSH". 
Per copiare la cartella contenente il progetto sulla macchina EC2 si utilizza, invece, il comando "scp -r -i path/to/fileName.pem path/to/project_folder_on_the_host ec2-user@ip_address:/home/ec2-user/destination_path. 
Per ottenere l'indirizzo IP dell'istanza EC2, dopo aver selezionato l'istanza in questione, ci si deve spostare nella finestra "EC2 instance connect".

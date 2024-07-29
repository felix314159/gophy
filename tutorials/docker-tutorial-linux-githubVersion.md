0. Build gophy binary statically linked (e.g. x86_64 and linux):
* ```CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build .```

1. Place resulting gophy binary in the docker folder of this repo. Also place key of Root Authority (raprivkey.key) in that folder if it is not already there.

2. Build image:
* ```docker build -t gophyimage .```

3. Run multiple nodes (3x light node + 3x full node + 1x RA):
* ```chmod +x runNodes.sh```
* ```./runNodes.sh```

----

Useful docker commands:

0. Get overview of all nodes (are they running? how long have they been running? what are the alias names?):
* ```docker ps -a```

1. View latest log of a specific node of interest (e.g. the node with alias "2f"):
* ```docker logs 2f```

2. The sh script runNodes configures the nodes (each node has its own docker container) to run interactively in detached mode. This means you can re-attach to a node to see its log in real-time. To re-attach e.g. to node "2f" use:
* ```docker attach 2f```

3. In order to detach from the node again (it will continue running but you won't see its real-time output anymore) you must first press Ctrl+P followed by Ctrl+Q

4. Gophy-specific command: If you want to run a command on a running node (e.g. dump information about the latest block of node "2f"), use the following command:
* ```docker exec -t 2f ./gophy -dump=latest```

----

Other helpful docker commands for managing existing images and containers:

5. Stop all docker containers (in context of gophy: stop all nodes):
* ```docker stop $(docker ps -a -q)```

6. Delete all stopped containers (running containers can't be deleted):
* ```docker container prune -f```

7. List docker images:
* ```docker images```

8. Delete ALL docker images (if at least one of its containers still exists this will not work, so in that case first stop all running containers, then delete them and only then use the following command:
* ```docker rmi $(docker images -q)```

9. Delete a single docker image by name (same restrictions as above apply), here e.g. delete the image called 'gophyimage':
* ```docker rm gophyimage```


#!/bin/bash
# This script starts new nodes FROM SCRATCH (any old blockchain data would be removed even if it would not prune all containers first).
# since each container name must be unique, first remove all old containers (do not run this script if you don't want this to happen) to later ensure no container of the same name exists already
yes | docker container prune -f
# create and run containers with different names and a matching terminal tab names (each container runs in bridged mode by default, but when testing i kept getting 'context timeout' issues when using /chat/ protocol. so let's use host networking)
#		first create three full nodes (to pass arguments to gophy just add the part "./gophy <yourArgument1> <yourArgument2>")
#                1. do not re-use same port (set httpPort to different value for each node, this is over which port this node would usually submit transactions)
# 				 2. Note about: --cap-add flag: it adds the linux capability NET_ADMIN so that these containers can interact with the networking stack [i left it in but it is not needed because i just use host and then have tc netem affect host. this option would be important however if you use bridged mode and then use separate tc netem settings for each container]
gnome-terminal --tab --title="1f" -- bash -c 'docker run --cap-add=NET_ADMIN --network host -itd --name 1f gophyimage ./gophy -syncMode=SyncMode_Initial_Full -httpPort=8091 -dockerAlias=1f'
gnome-terminal --tab --title="2f" -- bash -c 'docker run --cap-add=NET_ADMIN --network host -itd --name 2f gophyimage ./gophy -syncMode=SyncMode_Initial_Full -httpPort=8092 -dockerAlias=2f'
gnome-terminal --tab --title="3f" -- bash -c 'docker run --cap-add=NET_ADMIN --network host -itd --name 3f gophyimage ./gophy -syncMode=SyncMode_Initial_Full -httpPort=8093 -dockerAlias=3f'
#		then create three light nodes
gnome-terminal --tab --title="4l" -- bash -c 'docker run --cap-add=NET_ADMIN --network host -itd --name 4l gophyimage ./gophy -syncMode=SyncMode_Initial_Light -httpPort=8094 -dockerAlias=4l'
gnome-terminal --tab --title="5l" -- bash -c 'docker run --cap-add=NET_ADMIN --network host -itd --name 5l gophyimage ./gophy -syncMode=SyncMode_Initial_Light -httpPort=8095 -dockerAlias=5l'
gnome-terminal --tab --title="6l" -- bash -c 'docker run --cap-add=NET_ADMIN --network host -itd --name 6l gophyimage ./gophy -syncMode=SyncMode_Initial_Light -httpPort=8096 -dockerAlias=6l'
#		finally create the Root Authority (all nodes try to contact the RA during their initial sync so it is required to have any non-trivial activity)
gnome-terminal --tab --title="ra" -- bash -c 'docker run --cap-add=NET_ADMIN --network host -itd --name ra gophyimage ./gophy -syncMode=SyncMode_Continuous_Full -localKeyFile=true -raMode=true -raReset=true -pw=supersecretrapw -httpPort=8097 -dockerAlias=ra'


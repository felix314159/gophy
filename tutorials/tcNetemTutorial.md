* Tc-netem is Linux-only. I use the tool to simulate varying networking conditions to see how it affects gophy (e.g. how much packet loss can gophy handle before consensus starts breaking or before nodes fail to find each other etc.). In this short document I document some useful tc netem functions that can be used to test networking-related software under artifically introduced conditions. I found the manpages very difficult to understand (counter argument: manpages are not supposed to teach but are supposed to be a reference to someone who already understands what tc netem does), so I wrote this document to have my own simplified reference.

* Note 0: You need to know the name of your networking interface before you can continue. If you want to only have tc netem affect communication on the loopback interface (e.g. Docker nodes that run all use 'host' networking) then you likely want to affect the 'lo' interface. However, if you want tc netem to affect communication with external networks, then you need to check the name of your networking inferface using 'ip link show', which shows all interfaces. In my case, the other interface was called 'ens33' but other names like 'eth0' are also common.

* List of useful tc netem features:
	* delay (delay egress traffic)
	* loss (packet loss)
	* limit (how many packets can be queued before they are dropped)
	* duplicate (add packet duplicates)
	* corrupt (corrupt packets [1 bit is changed])
	* rate (limit egress traffic to simulate limited bandwidth)

* Show current tc netem setting [replace lo with whatever inferface you care about]:
	* ``` tc qdisc show dev lo ```

* Remove effects (revert to normal) [replace lo with whatever inferface you have affected]:
	* ``` sudo tc qdisc del dev lo root netem ```

---

Note 1: 'replace' is less frustrating to use than 'add' IMO because add gives errors when sth already exists. replace just adds it if it doesn't exist already and changes it if it does exist already.

Note 2: I added 'limit 10000' to all commands because the default of 1000 is a bit low. Limit means how many packets can be queued before they are discarded. This means that if you have e.g. a long delay set you might accidentally implicitely also create packet loss because the queue fills up and any further packets are just dropped. But I am using tc netem to be in control which effects occur so I want to avoid these hard to detect drops so I set the limit higher than default.

---

Example Scenarios:

* 100% packet loss:
	* ``` sudo tc qdisc replace dev lo root netem loss 100% limit 10000 ```

* 5% Loss + ca. 100 ms (+- 10ms) delay [jitter + biasing]:
	* ``` sudo tc qdisc replace dev lo root netem loss 5% delay 100ms 10ms 25% limit 10000 ```

* around 100 ms delay + 20% corruption: 
	* ``` sudo tc qdisc replace dev lo root netem delay 100ms 10ms 25% corrupt 20% limit 10000 ```

* 5% Loss + around 100 ms delay + 20% corruption + 8% duplicate: 
	* ``` sudo tc qdisc replace dev lo root netem loss 5% delay 100ms 10ms 25% corrupt 20% duplicate 8% limit 10000 ```

* 5% Loss + around 100 ms delay + 20% corruption + 8% duplicate + bandwidth limited to 1Mbit: 
	* ``` sudo tc qdisc replace dev lo root netem loss 5% delay 100ms 10ms 25% corrupt 20% duplicate 8% rate 1Mbit limit 10000 ```

* reordering of packets method 1 [by using delay]:
	* ``` sudo tc qdisc replace dev lo root netem gap 3 delay 100ms ```
	* Explanation: every 3rd packet is sent instantly, the first two are delayed which means there will be re-ordering

* reordering of packets method 2 [by using probability]:
	* ``` sudo tc qdisc replace dev lo root netem delay 100ms 10ms 25% reorder 80% ```
	* Explanation: 80% of packets are sent instantly, 100-80=20% are delayed which affects ordering

----

Note 3: 'replace' has an odd behavior in combination with 'reorder' that I don't understand. Example:
* Run these commands:
	* ``` sudo tc qdisc replace dev lo root netem loss 5% delay 100ms 10ms 25% limit 10000 ```
	* ``` sudo tc qdisc replace dev lo root netem loss 100% limit 10000 ```
	* ``` tc qdisc show dev lo ```

* Now you see the every setting from the first command is gone and only loss and limit are still there. But when you do:

	* ``` sudo tc qdisc del dev lo root netem ```
	* ``` sudo tc qdisc replace dev lo root netem loss 5% delay 100ms 10ms 25% limit 10000 reorder 80% ```
	* ``` sudo tc qdisc replace dev lo root netem loss 100% limit 10000 ```
	* ``` tc qdisc show dev lo ```

* Then you can see the reorder also survived when it shouldn't have. Is this a bug or a feature?

---

Relevant for Docker nodes: In the runNodes.sh I am using host networking so there is nothing to do, but when you use bridge networking then you must add:

* ``` --cap-add=NET_ADMIN ``` 

otherwise you would not be able to use tc netem for each node separately. I also think in that case you should not be using a scratch image because getting
tc netem to work in scratch probably requires further work.


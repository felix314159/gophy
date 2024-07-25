# scratch is reserved name for the smallest possible image (gophy is statically compiled and self-contained)
FROM scratch

# copy the gophy binary
ADD ./gophy ./gophy

# copy the RA private key
ADD ./raprivkey.key ./raprivkey.key

# copy the three html websites used for sending simtask, sending transaction and viewing performance stats (not existing folders like httpapi or monitoring will automatically be created by this command)
ADD ./submitSimtask.html ./httpapi/submitSimtask.html
ADD ./submitTransaction.html ./httpapi/submitTransaction.html
ADD ./performanceStats.html ./monitoring/performanceStats.html

# below could be used to run a command but you are supposed to use the runNodes.sh script for that
# CMD ["./gophy"]

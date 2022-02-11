# kubectl-run

A Kubernetes addon for running an ephemeral container with a "mounted" volume.

Example:
```
mkdir -p ./stuff
echo "Test suite" > ./stuff/in.txt
kubectl ran busybox -e VAR1=Hello -e VAR2=world -v ./stuff:/stuff -- sh -c 'cp /stuff/in.txt /stuff/out.txt && echo "$VAR1 $VAR2" >> /stuff/out.txt'
cat ./stuff/out.txt
```

It works by
1. Starting a container with `tail -f /dev/null` in a new pod. You can optionally specify environment variables.
2. Copies any "mounted" volumes into the container.
3. Runs the specified command.
4. Copies the "mounted" volumes back out of the container.
5. Deletes the pod.

Requires the image contain `tail` and `tar`.

Limitations:
- Volumes must be directories
- Source and destination basename for volumes must match. Source will be copied into the dirname of the destination.

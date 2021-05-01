# Docker Swarm autoscaler for Azure

Here is autoscaler and node drainer for Azure. The idea is simple: you can use autoscale rules on CPU or another metrics inside VM scaleset. Azure uses _Cloud-init_ for VM provisioning, so, you can add nodes to a Swarm cluster automatically with _Custom data and cloud init_

## Cloud-init example (Ubuntu 20.04)

```
#cloud-config
apt:
  sources:
    docker:
      source: "deb [arch=amd64] https://download.docker.com/linux/ubuntu  focal stable"
      keyid: 9DC858229FC7DD38854AE2D88D81803C0EBFCD88
package_upgrade: true
packages:
  - docker-ce
  - docker-ce-cli
  - containerd.io
runcmd:
  - sudo usermod -aG docker swarm
  - docker swarm join --token SWMTKN-1-1n7dilf18jfyefv6f60n0ddnmavxoyq3ue8flfb4k2gfpj5fv7-5rohtvmhac6bf8dfpc3do481w 10.1.0.4:2377
```

The scaler have two functions:

- "Autoscale" replicas when cluster got more nodes
- Drain and delete nodes when cluster scales down

## Autoscaling

Because of various best practices and ways to treat your microservices in Swarm I choose a simple way to achieve "autoscaling", it's merely _config.yaml_ where you can define how much replicas per node you want to have. Inside an example below I have service "docs" with two replicas per node. So, when your cluster is going to scale up - scaler increase how much replicas do you have.

```
services:
  docs: 2

```
## Scale down, node draining and rm

When your cluster is going to scale down scaler recieve a message from [API](https://docs.microsoft.com/en-us/azure/virtual-machines/windows/scheduled-events) drain node and remove it from cluster. Take a look to sources when you have concern about graceful shutdown timing. By default it is 30 seconds. 

## Prerequisites and some limitations: 

1) Azure has its own rules about hostnames and vm names. I expect your VMSS will have name like `swarm-xxx` where xxx could be a region or etc. 
2) You should change trigger event in sources if you would like to use SpotVM. 
3) By default config file location is `/home/config.yaml`
4) Sheduled Events in Azure should be enabled for VMSS.
5) Scaler do nothing when you manually reboot machines or some of it is down. 

## How to deploy

```
docker service create --mode global --constraint node.role==manager --mount src=/var/run/docker.sock,dst=/var/run/docker.sock,type=bind --name scaler --config source=config.yaml,target=/home/config.yaml codeandmedia/swarm-azure-scaler:latest
```
You may use my image for test purposes, but I highly recommend to customize image and sources for your cluster.

## Step-by-step how to get MultiGeoHAAutoscale-cluster

1) Create VM with white IP and VNET for the cluster, SSH to it and initialize `docker swarm init`
2) Add swarm join string to cloud-init and create two VMSS with 1 VM each in regions like Germany West Central and Switzerland North. Scale-in policy should be Newest-VM. Do not forget to enable SheduledEvents.
3) Setup your autoscale rules follow [the Docs](https://docs.microsoft.com/en-us/azure/virtual-machine-scale-sets/virtual-machine-scale-sets-autoscale-overview)
4) Create Basic Load Balancer for each ScaleSet and setup NSG, open ports related to your apps. Basic Load Balancers are free in Azure. 
5) Promote each first VM inside ScaleSat to managers.
6) Create config map for the scaler and deploy it.
7) Create and config your services.

You can use the VM with white IP to SSH to machines inside VMSS if you need. 
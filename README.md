#svc-lb-sync
##steps
1. when service type LoadBalancer is created in the hosted cluster, create service type LoadBalancer in the infra cluster pointing to the hosted cluster's virt-lunchers using the node port the service on the hosted cluster got
2. wait for the service in the infra cluster to get external IP
3. write the external IP to the service's status in the hosted cluster


# Load Balancer
A Go load balancer that monitors and notifies the status of endpoints through the terminal.
When one or more endpoints are down, the load balancer will route traffic to live endpoints and will send an ALERT DOWN message. 
When down endpoints are restored, the load balancer recognizes them and sends an ACTIVE message.
When all endpoints are down, the load balancer responds to requests with 502 Bad Gateway.

# Start Load Balancer and Endpoints
    source ./ENV.sh
    ./endpoints/start_endpoints.sh
    go run load_balancer.go

# Stop Load Balancer and Remove Endpoints
	./endpoints/stop_endpoint.sh
    kill -15 $(cat pid.txt)

# Behavior
All endpoints are active.
* Load Balancer uses a round robin to route traffic evenly.
* Prints all active status to terminal.
One or more endpoints are down.
* Load Balancer will route traffic to live endpoitns.
* Alerts to terminal which endpoints are down.
One or more endoints are restored.
* Load Balancer will detect restored endpoints and return them to the round robin.
* Updates to temrinmal the ACTIVE status of restored endpoints.
All endpoints are down.
* Load Balancer will now respond to all requests with 502 Bad Gateway.
* Alerts to terminal that all connections are down.
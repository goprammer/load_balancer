#!/bin/bash

podman run -dit --name box1 -p 127.0.0.1:8001:80 nginx
podman cp endpoints/box1.html box1:/usr/share/nginx/html/index.html

podman run -dit --name box2 -p 127.0.0.1:8002:80 nginx
podman cp endpoints/box2.html box2:/usr/share/nginx/html/index.html

podman run -dit --name box3 -p 127.0.0.1:8003:80 nginx
podman cp endpoints/box3.html box3:/usr/share/nginx/html/index.html
#!/bin/bash

podman stop box1 box2 box3
podman rm box1 box2 box3
podman rmi nginx
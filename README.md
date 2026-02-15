kubectl bad
===========

A simple tool to help you quickly find bad state of your Kubernetes cluster. It
checks for common issues such as:

- Pods in CrashLoopBackOff or Error state
- Nodes in NotReady state
- Deployments with unavailable replicas
- Replica sets with a deleted parent
- Services without endpoints
- PersistentVolumeClaims in Pending state


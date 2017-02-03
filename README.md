# gcp-backups
Automatic snapshots of Google Cloud Plateform Disk for Kubernetes and Docker

# Description

This program make snapshots of one or many disks, and delete old snapshots.

# Usage

## Compile

First compile the Go file: `go build backup.go`

## Execute locally

Then you can execute the program: `backup --filter "name = my-disk" --limit 5 --dry-run`

Set a filter for the `gcloud compute disks list` command, or set as `""` to create a snapshot for each disk found in the current cluster.

Set a limit of snapshot saved for each disk using the `--limit` flag: when there is more than `--limit` snapshots, they will be deleted.

Use `--dry-run` to watch logs of what will happen.

## On Google Cloud Platform

When executed on a Kubernetes cluster, the `gcloud` command will automatically find all the cluster's disks.

Best thing is to create a Docker image from `google/cloud-sdk` image and set a *cron job* for the backup. If you plan to do a backup every day, set a limit to 7 to keep only one week of snapshots.

**We will add a Docker image and an example as soon as possible ;)**

# Contributions

You are welcome to make any suggestions and Pull Requests!

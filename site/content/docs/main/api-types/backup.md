---
title: "Backup API Type"
layout: docs
---

## Use

Use the `Backup` API type to request the Velero server to perform a backup. Once created, the
Velero Server immediately starts the backup process.

## API GroupVersion

Backup belongs to the API group version `velero.io/v1`.

## Definition

Here is a sample `Backup` object with each of the fields documented:

```yaml
# Standard Kubernetes API Version declaration. Required.
apiVersion: velero.io/v1
# Standard Kubernetes Kind declaration. Required.
kind: Backup
# Standard Kubernetes metadata. Required.
metadata:
  # Backup name. May be any valid Kubernetes object name. Required.
  name: a
  # Backup namespace. Must be the namespace of the Velero server. Required.
  namespace: velero
# Parameters about the backup. Required.
spec:
  # Array of namespaces to include in the backup. If unspecified, all namespaces are included.
  # Optional.
  includedNamespaces:
  - '*'
  # Array of namespaces to exclude from the backup. Optional.
  excludedNamespaces:
  - some-namespace
  # Array of resources to include in the backup. Resources may be shortcuts (for example 'po' for 'pods')
  # or fully-qualified. If unspecified, all resources are included. Optional.
  includedResources:
  - '*'
  # Array of resources to exclude from the backup. Resources may be shortcuts (for example 'po' for 'pods')
  # or fully-qualified. Optional.
  excludedResources:
  - storageclasses.storage.k8s.io
  # Whether or not to include cluster-scoped resources. Valid values are true, false, and
  # null/unset. If true, all cluster-scoped resources are included (subject to included/excluded
  # resources and the label selector). If false, no cluster-scoped resources are included. If unset,
  # all cluster-scoped resources are included if and only if all namespaces are included and there are
  # no excluded namespaces. Otherwise, if there is at least one namespace specified in either
  # includedNamespaces or excludedNamespaces, then the only cluster-scoped resources that are backed
  # up are those associated with namespace-scoped resources included in the backup. For example, if a
  # PersistentVolumeClaim is included in the backup, its associated PersistentVolume (which is
  # cluster-scoped) would also be backed up.
  includeClusterResources: null
  # Individual objects must match this label selector to be included in the backup. Optional.
  labelSelector:
    matchLabels:
      app: velero
      component: server
  # Whether or not to snapshot volumes. This only applies to PersistentVolumes for Azure, GCE, and
  # AWS. Valid values are true, false, and null/unset. If unset, Velero performs snapshots as long as
  # a persistent volume provider is configured for Velero.
  snapshotVolumes: null
  # Where to store the tarball and logs.
  storageLocation: aws-primary
  # The list of locations in which to store volume snapshots created for this backup.
  volumeSnapshotLocations:
    - aws-primary
    - gcp-primary
  # The amount of time before this backup is eligible for garbage collection. If not specified,
  # a default value of 30 days will be used. The default can be configured on the velero server
  # by passing the flag --default-backup-ttl.
  ttl: 24h0m0s
  # Whether restic should be used to take a backup of all pod volumes by default.
  defaultVolumesToRestic: true
  # Actions to perform at different times during a backup. The only hook supported is
  # executing a command in a container in a pod using the pod exec API. Optional.
  hooks:
    # Array of hooks that are applicable to specific resources. Optional.
    resources:
      -
        # Name of the hook. Will be displayed in backup log.
        name: my-hook
        # Array of namespaces to which this hook applies. If unspecified, the hook applies to all
        # namespaces. Optional.
        includedNamespaces:
        - '*'
        # Array of namespaces to which this hook does not apply. Optional.
        excludedNamespaces:
        - some-namespace
        # Array of resources to which this hook applies. The only resource supported at this time is
        # pods.
        includedResources:
        - pods
        # Array of resources to which this hook does not apply. Optional.
        excludedResources: []
        # This hook only applies to objects matching this label selector. Optional.
        labelSelector:
          matchLabels:
            app: velero
            component: server
        # An array of hooks to run before executing custom actions. Only "exec" hooks are supported.
        pre:
          -
            # The type of hook. This must be "exec".
            exec:
              # The name of the container where the command will be executed. If unspecified, the
              # first container in the pod will be used. Optional.
              container: my-container
              # The command to execute, specified as an array. Required.
              command:
                - /bin/uname
                - -a
              # How to handle an error executing the command. Valid values are Fail and Continue.
              # Defaults to Fail. Optional.
              onError: Fail
              # How long to wait for the command to finish executing. Defaults to 30 seconds. Optional.
              timeout: 10s
        # An array of hooks to run after all custom actions and additional items have been
        # processed. Only "exec" hooks are supported.
        post:
          # Same content as pre above.
# Status about the Backup. Users should not set any data here.
status:
  # The version of this Backup. The only version supported is 1.
  version: 1
  # The date and time when the Backup is eligible for garbage collection.
  expiration: null
  # The current phase. Valid values are New, FailedValidation, InProgress, Completed, PartiallyFailed, Failed.
  phase: ""
  # An array of any validation errors encountered.
  validationErrors: null
  # Date/time when the backup started being processed.
  startTimestamp: 2019-04-29T15:58:43Z
  # Date/time when the backup finished being processed.
  completionTimestamp: 2019-04-29T15:58:56Z
  # Number of volume snapshots that Velero tried to create for this backup.
  volumeSnapshotsAttempted: 2
  # Number of volume snapshots that Velero successfully created for this backup.
  volumeSnapshotsCompleted: 1
  # Number of warnings that were logged by the backup.
  warnings: 2
  # Number of errors that were logged by the backup.
  errors: 0
  # An error that caused the entire backup to fail.
  failureReason: ""

```

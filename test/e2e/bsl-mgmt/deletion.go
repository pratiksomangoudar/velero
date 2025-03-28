/*
Copyright the Velero contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package bslmgmt

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/vmware-tanzu/velero/test/e2e"
	. "github.com/vmware-tanzu/velero/test/e2e/util/k8s"
	. "github.com/vmware-tanzu/velero/test/e2e/util/kibishii"

	. "github.com/vmware-tanzu/velero/test/e2e/util/providers"
	. "github.com/vmware-tanzu/velero/test/e2e/util/velero"
)

const (
	// Please make sure length of this namespace should be shorter,
	// otherwise ResticRepositories name verification will be wrong
	// when making combination of ResticRepositories name(max length is 63)
	bslDeletionTestNs = "bsl-deletion"
)

// Test backup and restore of Kibishi using restic

func BslDeletionWithSnapshots() {
	BslDeletionTest(true)
}

func BslDeletionWithRestic() {
	BslDeletionTest(false)
}
func BslDeletionTest(useVolumeSnapshots bool) {
	client, err := NewTestClient()
	Expect(err).To(Succeed(), "Failed to instantiate cluster client for backup deletion tests")
	less := func(a, b string) bool { return a < b }
	BeforeEach(func() {
		if useVolumeSnapshots && VeleroCfg.CloudProvider == "kind" {
			Skip("Volume snapshots not supported on kind")
		}
		var err error
		flag.Parse()
		UUIDgen, err = uuid.NewRandom()
		Expect(err).To(Succeed())
		if VeleroCfg.InstallVelero {
			Expect(VeleroInstall(context.Background(), &VeleroCfg, useVolumeSnapshots)).To(Succeed())
		}
	})

	AfterEach(func() {
		if VeleroCfg.InstallVelero {
			if !VeleroCfg.Debug {
				Expect(DeleteNamespace(context.Background(), client, bslDeletionTestNs,
					true)).To(Succeed(), fmt.Sprintf("failed to delete the namespace %q",
					bslDeletionTestNs))
				Expect(VeleroUninstall(context.Background(), VeleroCfg.VeleroCLI,
					VeleroCfg.VeleroNamespace)).To(Succeed())
			}
		}
	})

	When("kibishii is the sample workload", func() {
		It("Local backups and restic repos (if Velero was installed with Restic) will be deleted once the corresponding backup storage location is deleted", func() {
			if VeleroCfg.AdditionalBSLProvider == "" {
				Skip("no additional BSL provider given, not running multiple BackupStorageLocation with unique credentials tests")
			}

			if VeleroCfg.AdditionalBSLBucket == "" {
				Skip("no additional BSL bucket given, not running multiple BackupStorageLocation with unique credentials tests")
			}

			if VeleroCfg.AdditionalBSLCredentials == "" {
				Skip("no additional BSL credentials given, not running multiple BackupStorageLocation with unique credentials tests")
			}

			By(fmt.Sprintf("Add an additional plugin for provider %s", VeleroCfg.AdditionalBSLProvider), func() {
				Expect(VeleroAddPluginsForProvider(context.TODO(), VeleroCfg.VeleroCLI,
					VeleroCfg.VeleroNamespace, VeleroCfg.AdditionalBSLProvider,
					VeleroCfg.AddBSLPlugins, VeleroCfg.Features)).To(Succeed())
			})

			additionalBsl := fmt.Sprintf("bsl-%s", UUIDgen)
			secretName := fmt.Sprintf("bsl-credentials-%s", UUIDgen)
			secretKey := fmt.Sprintf("creds-%s", VeleroCfg.AdditionalBSLProvider)
			files := map[string]string{
				secretKey: VeleroCfg.AdditionalBSLCredentials,
			}

			By(fmt.Sprintf("Create Secret for additional BSL %s", additionalBsl), func() {
				Expect(CreateSecretFromFiles(context.TODO(), client, VeleroCfg.VeleroNamespace, secretName, files)).To(Succeed())
			})

			By(fmt.Sprintf("Create additional BSL using credential %s", secretName), func() {
				Expect(VeleroCreateBackupLocation(context.TODO(),
					VeleroCfg.VeleroCLI,
					VeleroCfg.VeleroNamespace,
					additionalBsl,
					VeleroCfg.AdditionalBSLProvider,
					VeleroCfg.AdditionalBSLBucket,
					VeleroCfg.AdditionalBSLPrefix,
					VeleroCfg.AdditionalBSLConfig,
					secretName,
					secretKey,
				)).To(Succeed())
			})

			backupName_1 := "backup1-" + UUIDgen.String()
			backupName_2 := "backup2-" + UUIDgen.String()
			oneHourTimeout, _ := context.WithTimeout(context.Background(), time.Minute*60)

			backupLocation_1 := "default"
			backupLocation_2 := additionalBsl
			podName_1 := "kibishii-deployment-0"
			podName_2 := "kibishii-deployment-1"

			label_1 := "for=1"
			// TODO remove when issue https://github.com/vmware-tanzu/velero/issues/4724 is fixed
			//label_2 := "for!=1"
			label_2 := "for=2"
			By("Create namespace for sample workload", func() {
				Expect(CreateNamespace(oneHourTimeout, client, bslDeletionTestNs)).To(Succeed())
			})

			By("Deploy sample workload of Kibishii", func() {
				Expect(KibishiiPrepareBeforeBackup(oneHourTimeout, client, VeleroCfg.CloudProvider,
					bslDeletionTestNs, VeleroCfg.RegistryCredentialFile, VeleroCfg.Features,
					VeleroCfg.KibishiiDirectory, useVolumeSnapshots)).To(Succeed())
			})

			// Restic can not backup PV only, so pod need to be labeled also
			By("Label all 2 worker-pods of Kibishii", func() {
				Expect(AddLabelToPod(context.Background(), podName_1, bslDeletionTestNs, label_1)).To(Succeed())
				Expect(AddLabelToPod(context.Background(), "kibishii-deployment-1", bslDeletionTestNs, label_2)).To(Succeed())
			})

			By("Get all 2 PVCs of Kibishii and label them seprately ", func() {
				pvc, err := GetPvcByPodName(context.Background(), bslDeletionTestNs, podName_1)
				Expect(err).To(Succeed())
				fmt.Println(pvc)
				Expect(len(pvc)).To(Equal(1))
				pvc1 := pvc[0]
				pvc, err = GetPvcByPodName(context.Background(), bslDeletionTestNs, podName_2)
				Expect(err).To(Succeed())
				fmt.Println(pvc)
				Expect(len(pvc)).To(Equal(1))
				pvc2 := pvc[0]
				Expect(AddLabelToPvc(context.Background(), pvc1, bslDeletionTestNs, label_1)).To(Succeed())
				Expect(AddLabelToPvc(context.Background(), pvc2, bslDeletionTestNs, label_2)).To(Succeed())
			})

			By(fmt.Sprintf("Backup one of PV of sample workload by label-1 - Kibishii by the first BSL %s", backupLocation_1), func() {
				// TODO currently, the upgrade case covers the upgrade path from 1.6 to main and the velero v1.6 doesn't support "debug" command
				// TODO move to "runDebug" after we bump up to 1.7 in the upgrade case
				Expect(VeleroBackupNamespace(oneHourTimeout, VeleroCfg.VeleroCLI,
					VeleroCfg.VeleroNamespace, backupName_1, bslDeletionTestNs,
					backupLocation_1, useVolumeSnapshots, label_1)).To(Succeed())
			})

			By(fmt.Sprintf("Back up the other one PV of sample workload with label-2 into the additional BSL %s", backupLocation_2), func() {
				Expect(VeleroBackupNamespace(oneHourTimeout, VeleroCfg.VeleroCLI,
					VeleroCfg.VeleroNamespace, backupName_2, bslDeletionTestNs,
					backupLocation_2, useVolumeSnapshots, label_2)).To(Succeed())
			})

			if useVolumeSnapshots {
				if VeleroCfg.CloudProvider == "vsphere" {
					// TODO - remove after upload progress monitoring is implemented
					By("Waiting for vSphere uploads to complete", func() {
						Expect(WaitForVSphereUploadCompletion(oneHourTimeout, time.Hour,
							bslDeletionTestNs)).To(Succeed())
					})
					By(fmt.Sprintf("Snapshot CR in backup %s should be created", backupName_1), func() {
						Expect(SnapshotCRsCountShouldBe(context.Background(), bslDeletionTestNs,
							backupName_1, 1)).To(Succeed())
					})
					By(fmt.Sprintf("Snapshot CR in backup %s should be created", backupName_2), func() {
						Expect(SnapshotCRsCountShouldBe(context.Background(), bslDeletionTestNs,
							backupName_2, 1)).To(Succeed())
					})
				}
				var snapshotCheckPoint SnapshotCheckPoint
				snapshotCheckPoint.NamespaceBackedUp = bslDeletionTestNs
				By(fmt.Sprintf("Snapshot of bsl %s should be created in cloud object store", backupLocation_1), func() {
					snapshotCheckPoint.ExpectCount = 1
					snapshotCheckPoint.PodName = podName_1
					Expect(SnapshotsShouldBeCreatedInCloud(VeleroCfg.CloudProvider,
						VeleroCfg.CloudCredentialsFile, VeleroCfg.BSLBucket,
						VeleroCfg.BSLConfig, backupName_1, snapshotCheckPoint)).To(Succeed())
				})
				By(fmt.Sprintf("Snapshot of bsl %s should be created in cloud object store", backupLocation_2), func() {
					snapshotCheckPoint.ExpectCount = 1
					snapshotCheckPoint.PodName = podName_2
					var BSLCredentials, BSLConfig string
					if VeleroCfg.CloudProvider == "vsphere" {
						BSLCredentials = VeleroCfg.AdditionalBSLCredentials
						BSLConfig = VeleroCfg.AdditionalBSLConfig
					} else {
						BSLCredentials = VeleroCfg.CloudCredentialsFile
						BSLConfig = VeleroCfg.BSLConfig
					}
					Expect(SnapshotsShouldBeCreatedInCloud(VeleroCfg.CloudProvider,
						BSLCredentials, VeleroCfg.AdditionalBSLBucket,
						BSLConfig, backupName_2, snapshotCheckPoint)).To(Succeed())
				})
			} else { // For Restics
				By(fmt.Sprintf("Resticrepositories for BSL %s should be created in Velero namespace", backupLocation_1), func() {
					Expect(ResticRepositoriesCountShouldBe(context.Background(),
						VeleroCfg.VeleroNamespace, bslDeletionTestNs+"-"+backupLocation_1, 1)).To(Succeed())
				})
				By(fmt.Sprintf("Resticrepositories for BSL %s should be created in Velero namespace", backupLocation_2), func() {
					Expect(ResticRepositoriesCountShouldBe(context.Background(),
						VeleroCfg.VeleroNamespace, bslDeletionTestNs+"-"+backupLocation_2, 1)).To(Succeed())
				})
			}

			By(fmt.Sprintf("Verify if backup %s is created or not", backupName_1), func() {
				Expect(WaitForBackupCreated(context.Background(), VeleroCfg.VeleroCLI,
					backupName_1, 10*time.Minute)).To(Succeed())
			})

			By(fmt.Sprintf("Verify if backup %s is created or not", backupName_2), func() {
				Expect(WaitForBackupCreated(context.Background(), VeleroCfg.VeleroCLI,
					backupName_2, 10*time.Minute)).To(Succeed())
			})

			backupsInBSL1, err := GetBackupsFromBsl(context.Background(), VeleroCfg.VeleroCLI, backupLocation_1)
			Expect(err).To(Succeed())
			backupsInBSL2, err := GetBackupsFromBsl(context.Background(), VeleroCfg.VeleroCLI, backupLocation_2)
			Expect(err).To(Succeed())
			backupsInBsl1AndBsl2 := append(backupsInBSL1, backupsInBSL2...)

			By(fmt.Sprintf("Get all backups from 2 BSLs %s before deleting one of them", backupLocation_1), func() {
				backupsBeforeDel, err := GetAllBackups(context.Background(), VeleroCfg.VeleroCLI)
				Expect(err).To(Succeed())
				Expect(cmp.Diff(backupsInBsl1AndBsl2, backupsBeforeDel, cmpopts.SortSlices(less))).Should(BeEmpty())

				By(fmt.Sprintf("Backup1 %s should exist in cloud object store before bsl deletion", backupName_1), func() {
					Expect(ObjectsShouldBeInBucket(VeleroCfg.CloudProvider, VeleroCfg.CloudCredentialsFile,
						VeleroCfg.BSLBucket, VeleroCfg.BSLPrefix, VeleroCfg.BSLConfig,
						backupName_1, BackupObjectsPrefix)).To(Succeed())
				})

				By(fmt.Sprintf("Delete one of backup locations - %s", backupLocation_1), func() {
					Expect(DeleteBslResource(context.Background(), VeleroCfg.VeleroCLI, backupLocation_1)).To(Succeed())
				})

				By("Get all backups from 2 BSLs after deleting one of them", func() {
					backupsAfterDel, err := GetAllBackups(context.Background(), VeleroCfg.VeleroCLI)
					Expect(err).To(Succeed())
					// Default BSL is deleted, so backups in additional BSL should be left only
					Expect(cmp.Diff(backupsInBSL2, backupsAfterDel, cmpopts.SortSlices(less))).Should(BeEmpty())
				})
			})

			By(fmt.Sprintf("Backup1 %s should still exist in cloud object store after bsl deletion", backupName_1), func() {
				Expect(ObjectsShouldBeInBucket(VeleroCfg.CloudProvider, VeleroCfg.CloudCredentialsFile,
					VeleroCfg.BSLBucket, VeleroCfg.BSLPrefix, VeleroCfg.BSLConfig,
					backupName_1, BackupObjectsPrefix)).To(Succeed())
			})

			// TODO: Choose additional BSL to be deleted as an new test case
			// By(fmt.Sprintf("Backup %s should still exist in cloud object store", backupName_2), func() {
			// 	Expect(ObjectsShouldBeInBucket(VeleroCfg.CloudProvider, VeleroCfg.AdditionalBSLCredentials,
			// 		VeleroCfg.AdditionalBSLBucket, VeleroCfg.AdditionalBSLPrefix, VeleroCfg.AdditionalBSLConfig,
			// 		backupName_2, BackupObjectsPrefix)).To(Succeed())
			// })

			if useVolumeSnapshots {
				if VeleroCfg.CloudProvider == "vsphere" {
					By(fmt.Sprintf("Snapshot in backup %s should still exist, because snapshot CR will be deleted 24 hours later if the status is a success", backupName_2), func() {
						Expect(SnapshotCRsCountShouldBe(context.Background(), bslDeletionTestNs,
							backupName_1, 1)).To(Succeed())
						Expect(SnapshotCRsCountShouldBe(context.Background(), bslDeletionTestNs,
							backupName_2, 1)).To(Succeed())
					})
				}
				var snapshotCheckPoint SnapshotCheckPoint
				snapshotCheckPoint.NamespaceBackedUp = bslDeletionTestNs
				By(fmt.Sprintf("Snapshot should not be deleted in cloud object store after deleting bsl %s", backupLocation_1), func() {
					snapshotCheckPoint.ExpectCount = 1
					snapshotCheckPoint.PodName = podName_1
					Expect(SnapshotsShouldBeCreatedInCloud(VeleroCfg.CloudProvider,
						VeleroCfg.CloudCredentialsFile, VeleroCfg.BSLBucket,
						VeleroCfg.BSLConfig, backupName_1, snapshotCheckPoint)).To(Succeed())
				})
				By(fmt.Sprintf("Snapshot should not be deleted in cloud object store after deleting bsl %s", backupLocation_2), func() {
					snapshotCheckPoint.ExpectCount = 1
					snapshotCheckPoint.PodName = podName_2
					var BSLCredentials, BSLConfig string
					if VeleroCfg.CloudProvider == "vsphere" {
						BSLCredentials = VeleroCfg.AdditionalBSLCredentials
						BSLConfig = VeleroCfg.AdditionalBSLConfig
					} else {
						BSLCredentials = VeleroCfg.CloudCredentialsFile
						BSLConfig = VeleroCfg.BSLConfig
					}
					Expect(SnapshotsShouldBeCreatedInCloud(VeleroCfg.CloudProvider,
						BSLCredentials, VeleroCfg.AdditionalBSLBucket,
						BSLConfig, backupName_2, snapshotCheckPoint)).To(Succeed())
				})
			} else { // For Restic
				By(fmt.Sprintf("Resticrepositories for BSL %s should be deleted in Velero namespace", backupLocation_1), func() {
					Expect(ResticRepositoriesCountShouldBe(context.Background(),
						VeleroCfg.VeleroNamespace, bslDeletionTestNs+"-"+backupLocation_1, 0)).To(Succeed())
				})
				By(fmt.Sprintf("Resticrepositories for BSL %s should still exist in Velero namespace", backupLocation_2), func() {
					Expect(ResticRepositoriesCountShouldBe(context.Background(),
						VeleroCfg.VeleroNamespace, bslDeletionTestNs+"-"+backupLocation_2, 1)).To(Succeed())
				})
			}
			fmt.Printf("|| EXPECTED || - Backup deletion test completed successfully\n")
		})
	})
}

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

package velero

import (
	"bytes"
	"context"
	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/wait"

	velerov1api "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	cliinstall "github.com/vmware-tanzu/velero/pkg/cmd/cli/install"
	"github.com/vmware-tanzu/velero/pkg/cmd/util/flag"
	veleroexec "github.com/vmware-tanzu/velero/pkg/util/exec"
	common "github.com/vmware-tanzu/velero/test/e2e/util/common"
)

const BackupObjectsPrefix = "backups"
const PluginsObjectsPrefix = "plugins"

var pluginsMatrix = map[string]map[string][]string{
	"v1.4": {
		"aws":       {"velero/velero-plugin-for-aws:v1.1.0"},
		"azure":     {"velero/velero-plugin-for-microsoft-azure:v1.1.2"},
		"vsphere":   {"velero/velero-plugin-for-aws:v1.1.0", "vsphereveleroplugin/velero-plugin-for-vsphere:v1.0.2"},
		"gcp":       {"velero/velero-plugin-for-gcp:v1.1.0"},
		"azure-csi": {"velero/velero-plugin-for-microsoft-azure:v1.1.2", "velero/velero-plugin-for-csi:v0.1.1 "},
	},
	"v1.5": {
		"aws":       {"velero/velero-plugin-for-aws:v1.1.0"},
		"azure":     {"velero/velero-plugin-for-microsoft-azure:v1.1.2"},
		"vsphere":   {"velero/velero-plugin-for-aws:v1.1.0", "vsphereveleroplugin/velero-plugin-for-vsphere:v1.1.1"},
		"gcp":       {"velero/velero-plugin-for-gcp:v1.1.0"},
		"azure-csi": {"velero/velero-plugin-for-microsoft-azure:v1.1.2", "velero/velero-plugin-for-csi:v0.1.2 "},
	},
	"v1.6": {
		"aws":       {"velero/velero-plugin-for-aws:v1.2.1"},
		"azure":     {"velero/velero-plugin-for-microsoft-azure:v1.2.1"},
		"vsphere":   {"velero/velero-plugin-for-aws:v1.2.1", "vsphereveleroplugin/velero-plugin-for-vsphere:v1.1.1"},
		"gcp":       {"velero/velero-plugin-for-gcp:v1.2.1"},
		"azure-csi": {"velero/velero-plugin-for-microsoft-azure:v1.3.0", "velero/velero-plugin-for-csi:v0.1.2 "},
	},
	"v1.7": {
		"aws":       {"velero/velero-plugin-for-aws:v1.3.0"},
		"azure":     {"velero/velero-plugin-for-microsoft-azure:v1.3.0"},
		"vsphere":   {"velero/velero-plugin-for-aws:v1.3.0", "vsphereveleroplugin/velero-plugin-for-vsphere:v1.3.0"},
		"gcp":       {"velero/velero-plugin-for-gcp:v1.3.0"},
		"azure-csi": {"velero/velero-plugin-for-microsoft-azure:v1.3.0", "velero/velero-plugin-for-csi:v0.2.0"},
	},
	"v1.8": {
		"aws":       {"velero/velero-plugin-for-aws:v1.4.0"},
		"azure":     {"velero/velero-plugin-for-microsoft-azure:v1.4.0"},
		"vsphere":   {"velero/velero-plugin-for-aws:v1.4.0", "vsphereveleroplugin/velero-plugin-for-vsphere:v1.3.1"},
		"gcp":       {"velero/velero-plugin-for-gcp:v1.4.0"},
		"azure-csi": {"velero/velero-plugin-for-microsoft-azure:v1.4.0", "velero/velero-plugin-for-csi:v0.2.0"},
	},
	"main": {
		"aws":       {"velero/velero-plugin-for-aws:main"},
		"azure":     {"velero/velero-plugin-for-microsoft-azure:main"},
		"vsphere":   {"velero/velero-plugin-for-aws:main", "vsphereveleroplugin/velero-plugin-for-vsphere:v1.3.1"},
		"gcp":       {"velero/velero-plugin-for-gcp:main"},
		"azure-csi": {"velero/velero-plugin-for-microsoft-azure:main", "velero/velero-plugin-for-csi:main"},
	},
}

func GetProviderPluginsByVersion(version, providerName, feature string) ([]string, error) {
	var cloudMap map[string][]string
	arr := strings.Split(version, ".")
	if len(arr) >= 3 {
		cloudMap = pluginsMatrix[arr[0]+"."+arr[1]]
	}
	if len(cloudMap) == 0 {
		cloudMap = pluginsMatrix["main"]
		if len(cloudMap) == 0 {
			return nil, errors.Errorf("fail to get plugins by version: main")
		}
	}
	if strings.EqualFold(providerName, "azure") && strings.EqualFold(feature, "EnableCSI") {
		providerName = "azure-csi"
	}
	plugins, ok := cloudMap[providerName]
	if !ok {
		return nil, errors.Errorf("fail to get plugins by version: %s and provider %s", version, providerName)
	}
	return plugins, nil
}

// getProviderVeleroInstallOptions returns Velero InstallOptions for the provider.
func getProviderVeleroInstallOptions(
	pluginProvider,
	credentialsFile,
	objectStoreBucket,
	objectStorePrefix,
	bslConfig,
	vslConfig string,
	plugins []string,
	features string,
) (*cliinstall.InstallOptions, error) {

	if credentialsFile == "" {
		return nil, errors.Errorf("No credentials were supplied to use for E2E tests")
	}

	realPath, err := filepath.Abs(credentialsFile)
	if err != nil {
		return nil, err
	}

	io := cliinstall.NewInstallOptions()
	// always wait for velero and restic pods to be running.
	io.Wait = true
	io.ProviderName = pluginProvider
	io.SecretFile = credentialsFile

	io.BucketName = objectStoreBucket
	io.Prefix = objectStorePrefix
	io.BackupStorageConfig = flag.NewMap()
	io.BackupStorageConfig.Set(bslConfig)

	io.VolumeSnapshotConfig = flag.NewMap()
	io.VolumeSnapshotConfig.Set(vslConfig)

	io.SecretFile = realPath
	io.Plugins = flag.NewStringArray(plugins...)
	io.Features = features
	return io, nil
}

// checkBackupPhase uses VeleroCLI to inspect the phase of a Velero backup.
func checkBackupPhase(ctx context.Context, veleroCLI string, veleroNamespace string, backupName string,
	expectedPhase velerov1api.BackupPhase) error {
	checkCMD := exec.CommandContext(ctx, veleroCLI, "--namespace", veleroNamespace, "backup", "get", "-o", "json",
		backupName)

	fmt.Printf("get backup cmd =%v\n", checkCMD)
	stdoutPipe, err := checkCMD.StdoutPipe()
	if err != nil {
		return err
	}

	jsonBuf := make([]byte, 16*1024) // If the YAML is bigger than 16K, there's probably something bad happening

	err = checkCMD.Start()
	if err != nil {
		return err
	}

	bytesRead, err := io.ReadFull(stdoutPipe, jsonBuf)

	if err != nil && err != io.ErrUnexpectedEOF {
		return err
	}
	if bytesRead == len(jsonBuf) {
		return errors.New("yaml returned bigger than max allowed")
	}

	jsonBuf = jsonBuf[0:bytesRead]
	err = checkCMD.Wait()
	if err != nil {
		return err
	}
	backup := velerov1api.Backup{}
	err = json.Unmarshal(jsonBuf, &backup)
	if err != nil {
		return err
	}
	if backup.Status.Phase != expectedPhase {
		return errors.Errorf("Unexpected backup phase got %s, expecting %s", backup.Status.Phase, expectedPhase)
	}
	return nil
}

// checkRestorePhase uses VeleroCLI to inspect the phase of a Velero restore.
func checkRestorePhase(ctx context.Context, veleroCLI string, veleroNamespace string, restoreName string,
	expectedPhase velerov1api.RestorePhase) error {
	checkCMD := exec.CommandContext(ctx, veleroCLI, "--namespace", veleroNamespace, "restore", "get", "-o", "json",
		restoreName)

	fmt.Printf("get restore cmd =%v\n", checkCMD)
	stdoutPipe, err := checkCMD.StdoutPipe()
	if err != nil {
		return err
	}

	jsonBuf := make([]byte, 16*1024) // If the YAML is bigger than 16K, there's probably something bad happening

	err = checkCMD.Start()
	if err != nil {
		return err
	}

	bytesRead, err := io.ReadFull(stdoutPipe, jsonBuf)

	if err != nil && err != io.ErrUnexpectedEOF {
		return err
	}
	if bytesRead == len(jsonBuf) {
		return errors.New("yaml returned bigger than max allowed")
	}

	jsonBuf = jsonBuf[0:bytesRead]
	err = checkCMD.Wait()
	if err != nil {
		return err
	}
	restore := velerov1api.Restore{}
	err = json.Unmarshal(jsonBuf, &restore)
	if err != nil {
		return err
	}
	if restore.Status.Phase != expectedPhase {
		return errors.Errorf("Unexpected restore phase got %s, expecting %s", restore.Status.Phase, expectedPhase)
	}
	return nil
}

// VeleroBackupNamespace uses the veleroCLI to backup a namespace.
func VeleroBackupNamespace(ctx context.Context, veleroCLI, veleroNamespace, backupName, namespace, backupLocation string,
	useVolumeSnapshots bool, selector string) error {
	args := []string{
		"--namespace", veleroNamespace,
		"create", "backup", backupName,
		"--include-namespaces", namespace,
		"--wait",
	}
	if selector != "" {
		args = append(args, "--selector", selector)
	}

	if useVolumeSnapshots {
		args = append(args, "--snapshot-volumes")
	} else {
		args = append(args, "--default-volumes-to-restic")
		// To workaround https://github.com/vmware-tanzu/velero-plugin-for-vsphere/issues/347 for vsphere plugin v1.1.1
		// if the "--snapshot-volumes=false" isn't specified explicitly, the vSphere plugin will always take snapshots
		// for the volumes even though the "--default-volumes-to-restic" is specified
		// TODO This can be removed if the logic of vSphere plugin bump up to 1.3
		args = append(args, "--snapshot-volumes=false")
	}
	if backupLocation != "" {
		args = append(args, "--storage-location", backupLocation)
	}

	return VeleroBackupExec(ctx, veleroCLI, veleroNamespace, backupName, args)
}

// VeleroBackupExcludeNamespaces uses the veleroCLI to backup a namespace.
func VeleroBackupExcludeNamespaces(ctx context.Context, veleroCLI string, veleroNamespace string, backupName string, excludeNamespaces []string) error {
	namespaces := strings.Join(excludeNamespaces, ",")
	args := []string{
		"--namespace", veleroNamespace, "create", "backup", backupName,
		"--exclude-namespaces", namespaces,
		"--default-volumes-to-restic", "--wait",
	}
	return VeleroBackupExec(ctx, veleroCLI, veleroNamespace, backupName, args)
}

// VeleroBackupIncludeNamespaces uses the veleroCLI to backup a namespace.
func VeleroBackupIncludeNamespaces(ctx context.Context, veleroCLI string, veleroNamespace string, backupName string, includeNamespaces []string) error {
	namespaces := strings.Join(includeNamespaces, ",")
	args := []string{
		"--namespace", veleroNamespace, "create", "backup", backupName,
		"--include-namespaces", namespaces,
		"--default-volumes-to-restic", "--wait",
	}
	return VeleroBackupExec(ctx, veleroCLI, veleroNamespace, backupName, args)
}

// VeleroRestore uses the VeleroCLI to restore from a Velero backup.
func VeleroRestore(ctx context.Context, veleroCLI string, veleroNamespace string, restoreName string, backupName string) error {
	args := []string{
		"--namespace", veleroNamespace, "create", "restore", restoreName,
		"--from-backup", backupName, "--wait",
	}
	return VeleroRestoreExec(ctx, veleroCLI, veleroNamespace, restoreName, args)
}

func VeleroRestoreExec(ctx context.Context, veleroCLI, veleroNamespace, restoreName string, args []string) error {
	if err := VeleroCmdExec(ctx, veleroCLI, args); err != nil {
		return err
	}
	return checkRestorePhase(ctx, veleroCLI, veleroNamespace, restoreName, velerov1api.RestorePhaseCompleted)
}

func VeleroBackupExec(ctx context.Context, veleroCLI string, veleroNamespace string, backupName string, args []string) error {
	if err := VeleroCmdExec(ctx, veleroCLI, args); err != nil {
		return err
	}
	return checkBackupPhase(ctx, veleroCLI, veleroNamespace, backupName, velerov1api.BackupPhaseCompleted)
}

func VeleroCmdExec(ctx context.Context, veleroCLI string, args []string) error {
	cmd := exec.CommandContext(ctx, veleroCLI, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("velero cmd =%v\n", cmd)
	err := cmd.Run()
	if err != nil {
		return err
	}
	return err
}

func VeleroBackupLogs(ctx context.Context, veleroCLI string, veleroNamespace string, backupName string) error {
	args := []string{
		"--namespace", veleroNamespace, "backup", "describe", backupName,
	}
	if err := VeleroCmdExec(ctx, veleroCLI, args); err != nil {
		return err
	}
	args = []string{
		"--namespace", veleroNamespace, "backup", "logs", backupName,
	}
	return VeleroCmdExec(ctx, veleroCLI, args)
}

func RunDebug(ctx context.Context, veleroCLI, veleroNamespace, backup, restore string) {
	output := fmt.Sprintf("debug-bundle-%d.tar.gz", time.Now().UnixNano())
	args := []string{"debug", "--namespace", veleroNamespace, "--output", output, "--verbose"}
	if len(backup) > 0 {
		args = append(args, "--backup", backup)
	}
	if len(restore) > 0 {
		args = append(args, "--restore", restore)
	}
	fmt.Printf("Generating the debug tarball at %s\n", output)
	if err := VeleroCmdExec(ctx, veleroCLI, args); err != nil {
		fmt.Println(errors.Wrapf(err, "failed to run the debug command"))
	}
}

func VeleroCreateBackupLocation(ctx context.Context,
	veleroCLI,
	veleroNamespace,
	name,
	objectStoreProvider,
	bucket,
	prefix,
	config,
	secretName,
	secretKey string,
) error {
	args := []string{
		"--namespace", veleroNamespace,
		"create", "backup-location", name,
		"--provider", objectStoreProvider,
		"--bucket", bucket,
	}

	if prefix != "" {
		args = append(args, "--prefix", prefix)
	}

	if config != "" {
		args = append(args, "--config", config)
	}

	if secretName != "" && secretKey != "" {
		args = append(args, "--credential", fmt.Sprintf("%s=%s", secretName, secretKey))
	}
	return VeleroCmdExec(ctx, veleroCLI, args)
}

func getProviderPlugins(ctx context.Context, veleroCLI, objectStoreProvider, providerPlugins, feature string) ([]string, error) {
	// Fetch the plugins for the provider before checking for the object store provider below.
	var plugins []string
	if len(providerPlugins) > 0 {
		plugins = strings.Split(providerPlugins, ",")
	} else {
		version, err := getVeleroVersion(ctx, veleroCLI, true)
		if err != nil {
			return nil, errors.WithMessage(err, "failed to get velero version")
		}
		plugins, err = GetProviderPluginsByVersion(version, objectStoreProvider, feature)
		if err != nil {
			return nil, errors.WithMessagef(err, "Fail to get plugin by provider %s and version %s", objectStoreProvider, version)
		}
	}
	return plugins, nil
}

// VeleroAddPluginsForProvider determines which plugins need to be installed for a provider and
// installs them in the current Velero installation, skipping over those that are already installed.
func VeleroAddPluginsForProvider(ctx context.Context, veleroCLI string, veleroNamespace string, provider string, addPlugins, feature string) error {
	plugins, err := getProviderPlugins(ctx, veleroCLI, provider, addPlugins, feature)
	fmt.Printf("addPlugins cmd =%v\n", addPlugins)
	fmt.Printf("provider cmd = %v\n", provider)
	fmt.Printf("plugins cmd = %v\n", plugins)
	if err != nil {
		return errors.WithMessage(err, "Failed to get plugins")
	}
	for _, plugin := range plugins {
		stdoutBuf := new(bytes.Buffer)
		stderrBuf := new(bytes.Buffer)

		installPluginCmd := exec.CommandContext(ctx, veleroCLI, "--namespace", veleroNamespace, "plugin", "add", plugin)
		fmt.Printf("installPluginCmd cmd =%v\n", installPluginCmd)
		installPluginCmd.Stdout = stdoutBuf
		installPluginCmd.Stderr = stderrBuf

		err := installPluginCmd.Run()

		fmt.Fprint(os.Stdout, stdoutBuf)
		fmt.Fprint(os.Stderr, stderrBuf)

		if err != nil {
			// If the plugin failed to install as it was already installed, ignore the error and continue
			// TODO: Check which plugins are already installed by inspecting `velero plugin get`
			if !strings.Contains(stderrBuf.String(), "Duplicate value") {
				return errors.WithMessagef(err, "error installing plugin %s", plugin)
			}
		}
	}

	return nil
}

// WaitForVSphereUploadCompletion waits for uploads started by the Velero Plug-in for vSphere to complete
// TODO - remove after upload progress monitoring is implemented
func WaitForVSphereUploadCompletion(ctx context.Context, timeout time.Duration, namespace string) error {
	err := wait.PollImmediate(time.Minute, timeout, func() (bool, error) {
		checkSnapshotCmd := exec.CommandContext(ctx, "kubectl",
			"get", "-n", namespace, "snapshots.backupdriver.cnsdp.vmware.com", "-o=jsonpath='{range .items[*]}{.spec.resourceHandle.name}{\"=\"}{.status.phase}{\"\\n\"}{end}'")
		fmt.Printf("checkSnapshotCmd cmd =%v\n", checkSnapshotCmd)
		stdout, stderr, err := veleroexec.RunCommand(checkSnapshotCmd)
		if err != nil {
			fmt.Print(stdout)
			fmt.Print(stderr)
			return false, errors.Wrap(err, "failed to verify")
		}
		lines := strings.Split(stdout, "\n")
		complete := true
		for _, curLine := range lines {
			fmt.Println(curLine)
			comps := strings.Split(curLine, "=")
			// SnapshotPhase represents the lifecycle phase of a Snapshot.
			// New - No work yet, next phase is InProgress
			// InProgress - snapshot being taken
			// Snapshotted - local snapshot complete, next phase is Protecting or SnapshotFailed
			// SnapshotFailed - end state, snapshot was not able to be taken
			// Uploading - snapshot is being moved to durable storage
			// Uploaded - end state, snapshot has been protected
			// UploadFailed - end state, unable to move to durable storage
			// Canceling - when the SanpshotCancel flag is set, if the Snapshot has not already moved into a terminal state, the
			//             status will move to Canceling.  The snapshot ID will be removed from the status status if has been filled in
			//             and the snapshot ID will not longer be valid for a Clone operation
			// Canceled - the operation was canceled, the snapshot ID is not valid
			// Canceled - the operation was canceled, the snapshot ID is not valid
			if len(comps) == 2 {
				phase := comps[1]
				switch phase {
				case "Uploaded":
				case "New", "InProgress", "Snapshotted", "Uploading":
					complete = false
				default:
					return false, fmt.Errorf("unexpected snapshot phase: %s", phase)
				}
			}
		}
		return complete, nil
	})

	return err
}

func GetVsphereSnapshotIDs(ctx context.Context, timeout time.Duration, namespace, podName string) ([]string, error) {
	checkSnapshotCmd := exec.CommandContext(ctx, "kubectl",
		"get", "-n", namespace, "snapshots.backupdriver.cnsdp.vmware.com", "-o=jsonpath='{range .items[*]}{.spec.resourceHandle.name}{\"=\"}{.status.snapshotID}{\"\\n\"}{end}'")
	fmt.Printf("checkSnapshotCmd cmd =%v\n", checkSnapshotCmd)
	stdout, stderr, err := veleroexec.RunCommand(checkSnapshotCmd)
	if err != nil {
		fmt.Print(stdout)
		fmt.Print(stderr)
		return nil, errors.Wrap(err, "failed to verify")
	}
	stdout = strings.Replace(stdout, "'", "", -1)
	lines := strings.Split(stdout, "\n")
	var result []string

	for _, curLine := range lines {
		fmt.Println("curLine:" + curLine)
		curLine = strings.Replace(curLine, "\n", "", -1)
		if len(curLine) == 0 {
			continue
		}
		if podName != "" && !strings.Contains(curLine, podName) {
			continue
		}
		snapshotID := curLine[strings.LastIndex(curLine, ":")+1:]
		fmt.Println("snapshotID:" + snapshotID)
		snapshotIDDec, _ := b64.StdEncoding.DecodeString(snapshotID)
		fmt.Println("snapshotIDDec:" + string(snapshotIDDec))
		result = append(result, string(snapshotIDDec))
	}
	fmt.Println(result)
	return result, nil
}

func getVeleroVersion(ctx context.Context, veleroCLI string, clientOnly bool) (string, error) {
	args := []string{"version", "--timeout", "60s"}
	if clientOnly {
		args = append(args, "--client-only")
	}
	cmd := exec.CommandContext(ctx, veleroCLI, args...)
	fmt.Println("Get Version Command:" + cmd.String())
	stdout, stderr, err := veleroexec.RunCommand(cmd)
	if err != nil {
		return "", errors.Wrapf(err, "failed to get velero version, stdout=%s, stderr=%s", stdout, stderr)
	}

	output := strings.Replace(stdout, "\n", " ", -1)
	fmt.Println("Version:" + output)
	resultCount := 3
	regexpRule := `(?i)client\s*:\s*version\s*:\s*(\S+).+server\s*:\s*version\s*:\s*(\S+)`
	if clientOnly {
		resultCount = 2
		regexpRule = `(?i)client\s*:\s*version\s*:\s*(\S+)`
	}
	regCompiler := regexp.MustCompile(regexpRule)
	versionMatches := regCompiler.FindStringSubmatch(output)
	if len(versionMatches) != resultCount {
		return "", errors.New("failed to parse velero version from output")
	}
	if !clientOnly {
		if versionMatches[1] != versionMatches[2] {
			return "", errors.New("velero server and client version are not matched")
		}
	}
	return versionMatches[1], nil
}

func CheckVeleroVersion(ctx context.Context, veleroCLI string, expectedVer string) error {
	tag := expectedVer
	tagInstalled, err := getVeleroVersion(ctx, veleroCLI, false)
	if err != nil {
		return errors.WithMessagef(err, "failed to get Velero version")
	}
	if strings.Trim(tag, " ") != strings.Trim(tagInstalled, " ") {
		return errors.New(fmt.Sprintf("velero version %s is not as expected %s", tagInstalled, tag))
	}
	fmt.Printf("Velero version %s is as expected %s\n", tagInstalled, tag)
	return nil
}

func InstallVeleroCLI(version string) (string, error) {
	name := "velero-" + version + "-" + runtime.GOOS + "-" + runtime.GOARCH
	postfix := ".tar.gz"
	tarball := name + postfix
	tempFile, err := getVeleroCliTarball("https://github.com/vmware-tanzu/velero/releases/download/" + version + "/" + tarball)
	if err != nil {
		return "", errors.WithMessagef(err, "failed to get Velero CLI tarball")
	}
	tempVeleroCliDir, err := ioutil.TempDir("", "velero-test")
	if err != nil {
		return "", errors.WithMessagef(err, "failed to create temp dir for tarball extraction")
	}

	cmd := exec.Command("tar", "-xvf", tempFile.Name(), "-C", tempVeleroCliDir)
	defer os.Remove(tempFile.Name())

	if _, err := cmd.Output(); err != nil {
		return "", errors.WithMessagef(err, "failed to extract file from velero CLI tarball")
	}
	return tempVeleroCliDir + "/" + name + "/velero", nil
}

func getVeleroCliTarball(cliTarballUrl string) (*os.File, error) {
	lastInd := strings.LastIndex(cliTarballUrl, "/")
	tarball := cliTarballUrl[lastInd+1:]

	resp, err := http.Get(cliTarballUrl)
	if err != nil {
		return nil, errors.WithMessagef(err, "failed to access Velero CLI tarball")
	}
	defer resp.Body.Close()

	tarballBuf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.WithMessagef(err, "failed to read buffer for tarball %s.", tarball)
	}
	tmpfile, err := ioutil.TempFile("", tarball)
	if err != nil {
		return nil, errors.WithMessagef(err, "failed to create temp file for tarball %s locally.", tarball)
	}

	if _, err := tmpfile.Write(tarballBuf); err != nil {
		return nil, errors.WithMessagef(err, "failed to write tarball file %s locally.", tarball)
	}

	return tmpfile, nil
}
func DeleteBackupResource(ctx context.Context, veleroCLI string, backupName string) error {
	args := []string{"backup", "delete", backupName, "--confirm"}

	cmd := exec.CommandContext(ctx, veleroCLI, args...)
	fmt.Println("Delete backup Command:" + cmd.String())
	stdout, stderr, err := veleroexec.RunCommand(cmd)
	if err != nil {
		return errors.Wrapf(err, "Fail to get delete backup, stdout=%s, stderr=%s", stdout, stderr)
	}

	output := strings.Replace(stdout, "\n", " ", -1)
	fmt.Println("Backup delete command output:" + output)

	args = []string{"backup", "get", backupName}

	retryTimes := 5
	for i := 1; i < retryTimes+1; i++ {
		cmd = exec.CommandContext(ctx, veleroCLI, args...)
		fmt.Printf("Try %d times to delete backup %s \n", i, cmd.String())
		stdout, stderr, err = veleroexec.RunCommand(cmd)
		if err != nil {
			if strings.Contains(stderr, "not found") {
				fmt.Printf("|| EXPECTED || - Backup %s was deleted successfully according to message %s\n", backupName, stderr)
				return nil
			}
			return errors.Wrapf(err, "Fail to perform get backup, stdout=%s, stderr=%s", stdout, stderr)
		}
		time.Sleep(1 * time.Minute)
	}
	return nil
}

func GetBackup(ctx context.Context, veleroCLI string, backupName string) (string, string, error) {
	args := []string{"backup", "get", backupName}
	cmd := exec.CommandContext(ctx, veleroCLI, args...)
	return veleroexec.RunCommand(cmd)
}

func IsBackupExist(ctx context.Context, veleroCLI string, backupName string) (bool, error) {
	out, outerr, err := GetBackup(ctx, veleroCLI, backupName)
	if err != nil {
		if err != nil {
			if strings.Contains(outerr, "not found") {
				return false, nil
			}
			return false, err
		}
	}
	fmt.Printf("Backup %s exist locally according to output %s", backupName, out)
	return true, nil
}

func WaitBackupDeleted(ctx context.Context, veleroCLI string, backupName string, timeout time.Duration) error {
	return wait.PollImmediate(10*time.Second, timeout, func() (bool, error) {
		if exist, err := IsBackupExist(ctx, veleroCLI, backupName); err != nil {
			return false, err
		} else {
			if exist {
				return false, nil
			} else {
				return true, nil
			}
		}
	})
}

func WaitForBackupCreated(ctx context.Context, veleroCLI string, backupName string, timeout time.Duration) error {
	return wait.PollImmediate(10*time.Second, timeout, func() (bool, error) {
		if exist, err := IsBackupExist(ctx, veleroCLI, backupName); err != nil {
			return false, err
		} else {
			if exist {
				return true, nil
			} else {
				return false, nil
			}
		}
	})
}
func GetBackupsFromBsl(ctx context.Context, veleroCLI, bslName string) ([]string, error) {
	args1 := []string{"get", "backups"}
	if strings.TrimSpace(bslName) != "" {
		args1 = append(args1, "-l", "velero.io/storage-location="+bslName)
	}
	CmdLine1 := &common.OsCommandLine{
		Cmd:  veleroCLI,
		Args: args1,
	}

	CmdLine2 := &common.OsCommandLine{
		Cmd:  "awk",
		Args: []string{"{print $1}"},
	}

	CmdLine3 := &common.OsCommandLine{
		Cmd:  "tail",
		Args: []string{"-n", "+2"},
	}

	return common.GetListBy2Pipes(ctx, *CmdLine1, *CmdLine2, *CmdLine3)
}

func GetAllBackups(ctx context.Context, veleroCLI string) ([]string, error) {
	return GetBackupsFromBsl(ctx, veleroCLI, "")
}
func DeleteBslResource(ctx context.Context, veleroCLI string, bslName string) error {
	args := []string{"backup-location", "delete", bslName, "--confirm"}

	cmd := exec.CommandContext(ctx, veleroCLI, args...)
	fmt.Println("Delete backup location Command:" + cmd.String())
	stdout, stderr, err := veleroexec.RunCommand(cmd)
	if err != nil {
		return errors.Wrapf(err, "Fail to get delete location, stdout=%s, stderr=%s", stdout, stderr)
	}

	output := strings.Replace(stdout, "\n", " ", -1)
	fmt.Println("Backup location delete command output:" + output)

	fmt.Println(stdout)
	fmt.Println(stderr)
	return nil
}

func SnapshotCRsCountShouldBe(ctx context.Context, namespace, backupName string, expectedCount int) error {

	checkSnapshotCmd := exec.CommandContext(ctx, "kubectl",
		"get", "-n", namespace, "snapshots.backupdriver.cnsdp.vmware.com", "-o=jsonpath='{range .items[*]}{.metadata.labels.velero\\.io\\/backup-name}{\"\\n\"}{end}'")
	fmt.Printf("checkSnapshotCmd cmd =%v\n", checkSnapshotCmd)
	stdout, stderr, err := veleroexec.RunCommand(checkSnapshotCmd)
	if err != nil {
		fmt.Print(stdout)
		fmt.Print(stderr)
		return errors.Wrap(err, fmt.Sprintf("Failed getting snapshot CR of backup %s in namespace %d", backupName, expectedCount))
	}
	count := 0
	stdout = strings.Replace(stdout, "'", "", -1)
	arr := strings.Split(stdout, "\n")
	for _, bn := range arr {
		fmt.Println("Snapshot CR:" + bn)
		if strings.Contains(bn, backupName) {
			count++
		}
	}
	if count == expectedCount {
		return nil
	} else {
		return errors.New(fmt.Sprintf("SnapshotCR count %d of backup %s in namespace %s is not as expected %d", count, backupName, namespace, expectedCount))
	}
}

func ResticRepositoriesCountShouldBe(ctx context.Context, veleroNamespace, targetNamespace string, expectedCount int) error {
	resticArr, err := GetResticRepositories(ctx, veleroNamespace, targetNamespace)
	if err != nil {
		return errors.Wrapf(err, "Fail to get GetResticRepositories")
	}
	if len(resticArr) == expectedCount {
		return nil
	} else {
		return errors.New(fmt.Sprintf("Resticrepositories count %d in namespace %s is not as expected %d", len(resticArr), targetNamespace, expectedCount))
	}
}

func GetResticRepositories(ctx context.Context, veleroNamespace, targetNamespace string) ([]string, error) {
	CmdLine1 := &common.OsCommandLine{
		Cmd:  "kubectl",
		Args: []string{"get", "-n", veleroNamespace, "resticrepositories"},
	}

	CmdLine2 := &common.OsCommandLine{
		Cmd:  "grep",
		Args: []string{targetNamespace},
	}

	CmdLine3 := &common.OsCommandLine{
		Cmd:  "awk",
		Args: []string{"{print $1}"},
	}

	return common.GetListBy2Pipes(ctx, *CmdLine1, *CmdLine2, *CmdLine3)
}

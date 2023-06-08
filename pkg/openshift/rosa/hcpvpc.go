package rosa

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/openshift/osde2e-common/internal/terraform"

	"github.com/hashicorp/terraform-exec/tfexec"
)

// vpc represents the details of an aws vpc
type vpc struct {
	privateSubnet     string
	publicSubnet      string
	nodePrivateSubnet string
}

// hpcVPCError represents the custom error
type hcpVPCError struct {
	action string
	err    error
}

// Error returns the formatted error message when hpcVPCError is invoked
func (h *hcpVPCError) Error() string {
	return fmt.Sprintf("%s hcp cluster vpc failed: %v", h.action, h.err)
}

// copyFile copies the srcFile provided to the destFile
func copyFile(srcFile, destFile string) error {
	srcReader, err := FS.Open(srcFile)
	if err != nil {
		return fmt.Errorf("error opening %s file: %w", srcFile, err)
	}
	defer srcReader.Close()

	destReader, err := os.Create(destFile)
	if err != nil {
		return fmt.Errorf("error creating runtime %s file: %w", destFile, err)
	}
	defer destReader.Close()

	_, err = io.Copy(destReader, srcReader)
	if err != nil {
		return fmt.Errorf("error copying source file to destination file: %w", err)
	}

	return nil
}

// createHostedControlPlaneVPC creates the aws vpc used for provisioning hosted control plane clusters
func (r *Provider) createHostedControlPlaneVPC(ctx context.Context, clusterName, awsRegion, workingDir string) (*vpc, error) {
	action := "create"
	var vpc vpc

	if clusterName == "" || awsRegion == "" || workingDir == "" {
		return nil, &hcpVPCError{action: action, err: errors.New("one or more parameters is empty")}
	}

	tf, err := terraform.New(ctx, workingDir)
	if err != nil {
		return nil, &hcpVPCError{action: action, err: fmt.Errorf("failed to construct terraform runner: %v", err)}
	}

	if err = tf.SetEnvVars(r.awsCredentials.CredentialsAsMap()); err != nil {
		return nil, &hcpVPCError{action: action, err: fmt.Errorf("failed to set terraform runner aws credentials (env vars): %v", err)}
	}

	defer func() {
		_ = tf.Uninstall(ctx)
	}()

	r.log.Info("Creating aws vpc", clusterNameLoggerKey, clusterName, awsRegionLoggerKey, awsRegion)

	err = copyFile("assets/setup-hcp-vpc.tf", fmt.Sprintf("%s/setup-hcp-vpc.tf", workingDir))
	if err != nil {
		return nil, &hcpVPCError{action: action, err: fmt.Errorf("failed to copy terraform file to working directory: %v", err)}
	}

	err = tf.Init(ctx)
	if err != nil {
		return nil, &hcpVPCError{action: action, err: fmt.Errorf("failed to perform terraform init: %v", err)}
	}

	err = tf.Plan(
		ctx,
		tfexec.Var(fmt.Sprintf("aws_region=%s", awsRegion)),
		tfexec.Var(fmt.Sprintf("cluster_name=%s", clusterName)),
	)
	if err != nil {
		return nil, &hcpVPCError{action: action, err: fmt.Errorf("failed to perform terraform plan: %v", err)}
	}

	err = tf.Apply(ctx)
	if err != nil {
		return nil, &hcpVPCError{action: action, err: fmt.Errorf("failed to perform terraform apply: %v", err)}
	}

	output, err := tf.Output(ctx)
	if err != nil {
		return nil, &hcpVPCError{action: action, err: fmt.Errorf("failed to perform terraform output: %v", err)}
	}

	vpc.privateSubnet = strings.ReplaceAll(string(output["cluster-private-subnet"].Value), "\"", "")
	vpc.publicSubnet = strings.ReplaceAll(string(output["cluster-public-subnet"].Value), "\"", "")
	vpc.nodePrivateSubnet = strings.ReplaceAll(string(output["node-private-subnet"].Value), "\"", "")

	r.log.Info("AWS vpc created!", clusterNameLoggerKey, clusterName, terraformWorkingDirLoggerKey, workingDir)

	return &vpc, err
}

// deleteHostedControlPlaneVPC deletes the aws vpc used for provisioning hosted control plane clusters
func (r *Provider) deleteHostedControlPlaneVPC(ctx context.Context, clusterName, awsRegion, workingDir string) error {
	const action = "delete"

	if clusterName == "" || awsRegion == "" || workingDir == "" {
		return &hcpVPCError{action: action, err: errors.New("one or more parameters is empty")}
	}

	tf, err := terraform.New(ctx, workingDir)
	if err != nil {
		return &hcpVPCError{action: action, err: fmt.Errorf("failed to construct terraform runner: %v", err)}
	}

	if err = tf.SetEnvVars(r.awsCredentials.CredentialsAsMap()); err != nil {
		return &hcpVPCError{action: action, err: fmt.Errorf("failed to set terraform runner aws credentials (env vars): %v", err)}
	}

	defer func() {
		_ = tf.Uninstall(ctx)
	}()

	r.log.Info("Deleting aws vpc", clusterNameLoggerKey, clusterName, awsRegionLoggerKey, awsRegion, terraformWorkingDirLoggerKey, workingDir)

	err = tf.Init(ctx)
	if err != nil {
		return &hcpVPCError{action: action, err: fmt.Errorf("failed to perform terraform init: %v", err)}
	}

	err = tf.Destroy(
		ctx,
		tfexec.Var(fmt.Sprintf("aws_region=%s", awsRegion)),
		tfexec.Var(fmt.Sprintf("cluster_name=%s", clusterName)),
	)
	if err != nil {
		return &hcpVPCError{action: action, err: fmt.Errorf("failed to perform terraform destroy: %v", err)}
	}

	r.log.Info("AWS vpc deleted!", clusterNameLoggerKey, clusterName)

	return err
}

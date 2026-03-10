// terraform.go — terraform_plan tool.
// Runs `terraform init` + `terraform plan -no-color` in a nexus service's
// Terraform environment directory and returns the plan output.
// This is READ-ONLY — plan only, no apply.
package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jredh-dev/nexus/internal/mcp"
)

// nexusRoot is the path to the nexus repo inside the container.
// Mounted at /nexus by the docker-compose service definition.
const nexusRoot = "/nexus"

// knownServices is the list of nexus services that have Terraform configurations.
// Path: nexus/services/<name>/terraform/environments/dev/
var knownServices = []string{"portal", "deadman", "web", "cal"}

func registerTerraformTools(s *mcp.Server) {
	s.RegisterTool(mcp.Tool{
		Name:        "terraform_plan",
		Description: "Run `terraform plan` (read-only, no apply) for a nexus service and return the diff. Requires the nexus repo to be mounted at /nexus.",
		InputSchema: mcp.ToolSchema{
			Type: "object",
			Properties: map[string]mcp.SchemaProperty{
				"service": {
					Type:        "string",
					Description: fmt.Sprintf("Nexus service name to plan. One of: %s", strings.Join(knownServices, ", ")),
				},
				"env": {
					Type:        "string",
					Description: "Terraform environment directory under services/<service>/terraform/environments/ (default: dev)",
					Default:     "dev",
				},
			},
			Required: []string{"service"},
		},
	}, handleTerraformPlan)
}

type terraformPlanArgs struct {
	Service string `json:"service"`
	Env     string `json:"env"`
}

func handleTerraformPlan(raw json.RawMessage) (*mcp.ToolCallResult, error) {
	var args terraformPlanArgs
	if err := parseArgs(raw, &args); err != nil {
		return nil, err
	}
	if args.Service == "" {
		return nil, errMissing("service")
	}
	if args.Env == "" {
		args.Env = "dev"
	}

	// Validate service name — prevent path traversal.
	if !isKnownService(args.Service) {
		return nil, fmt.Errorf("unknown service %q — must be one of: %s", args.Service, strings.Join(knownServices, ", "))
	}

	// Resolve terraform directory path.
	tfDir := filepath.Join(nexusRoot, "services", args.Service, "terraform", "environments", args.Env)
	if _, err := os.Stat(tfDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("terraform directory does not exist: %s", tfDir)
	}

	result, err := runTerraformPlan(tfDir, args.Service, args.Env)
	if err != nil {
		return nil, err
	}
	return jsonResult(result)
}

// isKnownService checks that the service name is in the known list (path-traversal guard).
func isKnownService(name string) bool {
	for _, s := range knownServices {
		if s == name {
			return true
		}
	}
	return false
}

// terraformPlanResult is the structured output returned to the MCP caller.
type terraformPlanResult struct {
	Service    string `json:"service"`
	Env        string `json:"env"`
	TFDir      string `json:"tf_dir"`
	InitOut    string `json:"init_output"`
	PlanOut    string `json:"plan_output"`
	HasChanges bool   `json:"has_changes"`
	Duration   string `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// runTerraformPlan executes terraform init + plan in the given directory and
// returns a structured result. Plan failures are captured in the result (not
// returned as errors) so the caller sees the full output.
func runTerraformPlan(tfDir, service, env string) (terraformPlanResult, error) {
	start := time.Now()
	result := terraformPlanResult{
		Service: service,
		Env:     env,
		TFDir:   tfDir,
	}

	// Step 1: terraform init -backend=true -reconfigure -input=false
	initOut, initErr := runCmd(tfDir, "terraform",
		"init", "-backend=true", "-reconfigure", "-input=false", "-no-color")
	result.InitOut = initOut
	if initErr != nil {
		result.Error = fmt.Sprintf("terraform init failed: %v", initErr)
		result.Duration = fmt.Sprintf("%d", time.Since(start).Milliseconds())
		return result, nil // return result with error field set, not a Go error
	}

	// Step 2: terraform plan -no-color -input=false
	// Exit code 0 = no changes, 2 = changes present, other = error.
	planOut, planErr := runCmd(tfDir, "terraform",
		"plan", "-no-color", "-input=false", "-detailed-exitcode")
	result.PlanOut = planOut

	if planErr != nil {
		// Exit code 2 means changes are present — not a true error.
		if exitErr, ok := planErr.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
			result.HasChanges = true
		} else {
			result.Error = fmt.Sprintf("terraform plan failed: %v", planErr)
		}
	} else {
		result.HasChanges = false
	}

	result.Duration = fmt.Sprintf("%d", time.Since(start).Milliseconds())
	return result, nil
}

// runCmd executes a command in dir and returns combined stdout+stderr output.
func runCmd(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir

	// Pass through GCP credentials env var so Terraform can authenticate.
	cmd.Env = append(os.Environ(),
		"TF_IN_AUTOMATION=1",    // suppress interactive prompts
		"TF_CLI_ARGS=-no-color", // keep output clean
	)

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	return buf.String(), err
}

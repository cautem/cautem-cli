package service

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/cautem/quarshell-cli/internal/storage/gwconfig"
	"github.com/cautem/quarshell-cli/internal/storage/templates"
	"github.com/cautem/quarshell-sdk/go/cautem"
)

// TemplateCreate saves a local sandbox template.
func (a *App) TemplateCreate(t templates.Template) error {
	if t.Image != "" && t.From != "" {
		return fmt.Errorf("template create: use either --image or --from, not both")
	}
	if err := validateSandboxCreateResources(SandboxCreateOpts{
		CPU: t.CPU, Memory: t.Memory, GPU: t.GPUCount > 0, GPUCount: t.GPUCount, PidsLimit: t.PidsLimit,
	}); err != nil {
		return fmt.Errorf("template create: %w", err)
	}
	if err := validateDriverConfigJSON(t.DriverConfigJSON); err != nil {
		return fmt.Errorf("template create --driver-config-json: %w", err)
	}
	if err := templates.Save(t); err != nil {
		return err
	}
	if c, err := a.gatewayClient(); err == nil {
		ctx, cancel := a.withTimeout(TimeoutAPIShort)
		defer cancel()
		if _, err := c.Healthz(ctx); err == nil {
			b, _ := json.Marshal(t)
			_ = b // gateway templates optional; local is source of truth for CLI
		}
	}
	fmt.Printf("template %s saved\n", t.Name)
	return nil
}

// TemplateList lists local templates.
func (a *App) TemplateList() error {
	list, err := templates.List()
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("templates: (none)")
		return nil
	}
	for _, t := range list {
		fmt.Printf("%s\timage=%s from=%s\n", t.Name, t.Image, t.From)
	}
	return nil
}

// TemplateGet prints one template as JSON.
func (a *App) TemplateGet(name string) error {
	t, err := templates.Get(name)
	if err != nil {
		return err
	}
	return writeJSON(t)
}

// TemplateDelete removes a local template.
func (a *App) TemplateDelete(name string) error {
	if err := templates.Delete(name); err != nil {
		return err
	}
	fmt.Printf("template %s deleted\n", name)
	return nil
}

// GatewayTemplateCreate stores a template in the selected Gateway workspace.
func (a *App) GatewayTemplateCreate(t templates.Template) error {
	if err := validateGatewayTemplate(t); err != nil {
		return err
	}
	image, err := gwconfig.ResolveImage(t.From, t.Image)
	if err != nil {
		return err
	}
	if image == "" {
		return fmt.Errorf("gateway template create: --image or --from is required")
	}
	url, workspace, err := a.templateGatewayTarget()
	if err != nil {
		return err
	}
	client := a.clientFor(url)
	defer client.Close()
	ctx, cancel := a.withTimeout(TimeoutAPIShort)
	defer cancel()
	gpu := t.GPUCount > 0
	cpu := ""
	if t.CPU > 0 {
		cpu = strconv.FormatFloat(t.CPU, 'f', -1, 64)
	}
	created, err := client.CreateSandboxTemplate(ctx, cautem.WorkloadTemplate{
		Name: t.Name, Workspace: workspace, Image: image, Environment: t.Env,
		CPU: cpu, Memory: t.Memory,
		GPU: gpu, GPUCount: uint32(t.GPUCount), Labels: t.Labels, DriverConfigJSON: t.DriverConfigJSON,
	})
	if err != nil {
		return err
	}
	fmt.Printf("gateway template %s saved (workspace=%s)\n", created.Name, created.Workspace)
	return nil
}

// GatewayTemplateList lists templates from the selected Gateway workspace.
func (a *App) GatewayTemplateList() error {
	url, workspace, err := a.templateGatewayTarget()
	if err != nil {
		return err
	}
	client := a.clientFor(url)
	defer client.Close()
	ctx, cancel := a.withTimeout(TimeoutAPIShort)
	defer cancel()
	list, err := client.ListSandboxTemplates(ctx, workspace)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Printf("gateway templates: (none) workspace=%s\n", workspace)
		return nil
	}
	for _, template := range list {
		fmt.Printf("%s\timage=%s\tworkspace=%s\n", template.Name, template.Image, template.Workspace)
	}
	return nil
}

// GatewayTemplateGet prints a workspace-scoped Gateway template as JSON.
func (a *App) GatewayTemplateGet(name string) error {
	url, workspace, err := a.templateGatewayTarget()
	if err != nil {
		return err
	}
	client := a.clientFor(url)
	defer client.Close()
	ctx, cancel := a.withTimeout(TimeoutAPIShort)
	defer cancel()
	template, err := client.GetSandboxTemplate(ctx, workspace, name)
	if err != nil {
		return err
	}
	return writeJSON(template)
}

// GatewayTemplateDelete deletes only the named template in the selected workspace.
func (a *App) GatewayTemplateDelete(name string) error {
	url, workspace, err := a.templateGatewayTarget()
	if err != nil {
		return err
	}
	client := a.clientFor(url)
	defer client.Close()
	ctx, cancel := a.withTimeout(TimeoutAPIShort)
	defer cancel()
	deleted, err := client.DeleteSandboxTemplate(ctx, workspace, name)
	if err != nil {
		return err
	}
	if !deleted {
		return fmt.Errorf("gateway template %q not found in workspace %q", name, workspace)
	}
	fmt.Printf("gateway template %s deleted (workspace=%s)\n", name, workspace)
	return nil
}

func (a *App) templateGatewayTarget() (url, workspace string, err error) {
	url, err = a.currentGatewayURL()
	if err != nil {
		return "", "", err
	}
	if url == "" {
		return "", "", fmt.Errorf("no Gateway selected (use cautem gateway select or --gateway URL)")
	}
	_, workspace = a.profileScope()
	if workspace == "" {
		workspace = "default"
	}
	return url, workspace, nil
}

func validateGatewayTemplate(t templates.Template) error {
	if t.Policy != "" || t.PidsLimit != 0 || len(t.Providers) > 0 || len(t.Forwards) > 0 {
		return fmt.Errorf("gateway template: --policy, --pids-limit, --provider and --forward are not supported by the shared sandbox template format")
	}
	if t.Image != "" && t.From != "" {
		return fmt.Errorf("gateway template: use either --image or --from, not both")
	}
	if err := validateSandboxCreateResources(SandboxCreateOpts{
		CPU: t.CPU, Memory: t.Memory, GPU: t.GPUCount > 0, GPUCount: t.GPUCount,
	}); err != nil {
		return fmt.Errorf("gateway template: %w", err)
	}
	return validateDriverConfigJSON(t.DriverConfigJSON)
}

func writeJSON(value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(b, '\n'))
	return err
}

// mergeTemplateIntoCreate applies template defaults before CLI flags (template first).
func mergeTemplateIntoCreate(opt *SandboxCreateOpts, tpl templates.Template) {
	if tpl.Image != "" && opt.Image == "" {
		opt.Image = tpl.Image
	}
	if tpl.From != "" && opt.From == "" {
		opt.From = tpl.From
	}
	if tpl.Policy != "" && opt.Policy == "" {
		opt.Policy = tpl.Policy
	}
	if tpl.CPU > 0 && opt.CPU == 0 {
		opt.CPU = tpl.CPU
	}
	if tpl.Memory != "" && opt.Memory == "" {
		opt.Memory = tpl.Memory
	}
	if tpl.GPUCount > 0 && opt.GPUCount == 0 {
		opt.GPU = true
		opt.GPUCount = tpl.GPUCount
	}
	if tpl.DriverConfigJSON != "" && opt.DriverConfigJSON == "" {
		opt.DriverConfigJSON = tpl.DriverConfigJSON
	}
	if tpl.PidsLimit != 0 && opt.PidsLimit == 0 {
		opt.PidsLimit = tpl.PidsLimit
	}
	if len(tpl.Providers) > 0 && len(opt.Providers) == 0 {
		opt.Providers = append([]string{}, tpl.Providers...)
	}
	if tpl.Env != nil {
		if opt.Env == nil {
			opt.Env = map[string]string{}
		}
		for k, v := range tpl.Env {
			if _, ok := opt.Env[k]; !ok {
				opt.Env[k] = v
			}
		}
	}
	if tpl.Labels != nil {
		if opt.Labels == nil {
			opt.Labels = map[string]string{}
		}
		for k, v := range tpl.Labels {
			if _, ok := opt.Labels[k]; !ok {
				opt.Labels[k] = v
			}
		}
	}
	opt.Forwards = append(opt.Forwards, tpl.Forwards...)
}

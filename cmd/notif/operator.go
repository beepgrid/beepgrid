package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/notif-dev/notif/sdk/go/notif"
	"os"
	"path/filepath"
)

func operatorClient() (*notif.Client, error) {
	token, err := clientToken(true)
	if err != nil {
		return nil, err
	}
	return notif.New(env("NOTIF_URL", "http://localhost:4010"), token), nil
}
func seed(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("seed", flag.ContinueOnError)
	prefix := f.String("prefix", "examples/", "Allowed agent namespace")
	name := f.String("name", "Local launcher", "Software service name")
	out := f.String("out", ".local/launcher-token", "New 0600 credential file, never overwritten")
	if e := f.Parse(args); e != nil {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(*out), 0700); e != nil {
		return e
	}
	file, e := os.OpenFile(*out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	success := false
	defer func() {
		file.Close()
		if !success {
			_ = os.Remove(*out)
		}
	}()
	c, e := operatorClient()
	if e != nil {
		return e
	}
	var me struct {
		Person notif.Person `json:"person"`
		Admin  bool         `json:"admin"`
	}
	if e = c.Do(ctx, "GET", "/api/v1/me", nil, &me, ""); e != nil {
		return e
	}
	if !me.Admin {
		return fmt.Errorf("seed requires an administrator credential")
	}
	var services []struct {
		ID       string `json:"id"`
		External string `json:"external_id"`
	}
	if e = c.Do(ctx, "GET", "/api/v1/services", nil, &services, ""); e != nil {
		return e
	}
	external := "launcher/" + *prefix
	id := ""
	for _, s := range services {
		if s.External == external {
			id = s.ID
		}
	}
	if id == "" {
		var service struct {
			ID string `json:"id"`
		}
		if e = c.Do(ctx, "POST", "/api/v1/services", map[string]string{"external_id": external, "name": *name}, &service, ""); e != nil {
			return e
		}
		id = service.ID
	}
	var roles []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if e = c.Do(ctx, "GET", "/api/v1/roles", nil, &roles, ""); e != nil {
		return e
	}
	roleID := ""
	for _, r := range roles {
		if r.Name == "Launcher "+external {
			roleID = r.ID
		}
	}
	var role struct {
		ID string `json:"id"`
	}
	rules := []map[string]string{}
	for _, action := range []string{"view", "message", "ask"} {
		rules = append(rules, map[string]string{"action": action, "target_kind": "person", "target_id": me.Person.ID})
	}
	rules = append(rules, map[string]string{"action": "ask", "target_kind": "*", "target_id": "$self"})
	if e = c.Do(ctx, "POST", "/api/v1/roles", map[string]any{"id": roleID, "name": "Launcher " + external, "rules": rules}, &role, ""); e != nil {
		return e
	}
	if e = c.Do(ctx, "POST", "/api/v1/role-bindings", map[string]string{"role_id": role.ID, "subject_kind": "principal", "subject_id": id}, nil, ""); e != nil {
		return e
	}
	var credential struct {
		Token string `json:"token"`
	}
	if e = c.Do(ctx, "POST", "/api/v1/credentials", map[string]string{"principal_id": id, "kind": "enrollment", "prefix": *prefix}, &credential, ""); e != nil {
		return e
	}
	if _, e = file.WriteString(credential.Token + "\n"); e != nil {
		return e
	}
	if e = file.Sync(); e != nil {
		return e
	}
	if e = file.Close(); e != nil {
		return e
	}
	success = true
	fmt.Println("Launcher credential file:", *out)
	fmt.Println("Allowed namespace:", *prefix)
	fmt.Println("May contact only person:", me.Person.ID)
	return nil
}
func trace(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: notif trace REQUEST_ID")
	}
	c, e := operatorClient()
	if e != nil {
		return e
	}
	r, e := c.GetRequest(ctx, args[0])
	if e != nil {
		return e
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

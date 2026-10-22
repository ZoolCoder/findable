package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/zoolcoder/findable/internal/config"
	"github.com/zoolcoder/findable/internal/engine"
	"github.com/zoolcoder/toolshell/doctor"
)

// runDoctor checks the install: the binary on PATH, the config, and that
// every target's live state can be read.
func runDoctor(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { fmt.Fprint(stderr, usage()) }
	path := flags.String("config", "", "config file")
	asJSON := flags.Bool("json", false, "machine-readable report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	*path = findConfig(*path)

	r := doctor.New(version)
	r.AddCheck(doctor.Binary(name))
	cfg, err := config.Load(*path, nil)
	r.Err("config", err, fmt.Sprintf("%s, %d target(s)", *path, len(cfg.Targets)))
	if err == nil {
		eng, err := engine.New(cfg, engine.Default(), nil)
		if err != nil {
			r.Add("targets", doctor.Fail, err.Error())
		} else {
			for _, st := range eng.Status(context.Background()) {
				if st.Error != "" {
					r.Add("target "+st.Target, doctor.Fail, st.Error)
				} else {
					r.Add("target "+st.Target, doctor.OK, st.Summary)
				}
			}
		}
	}
	return r.Write(stdout, *asJSON)
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

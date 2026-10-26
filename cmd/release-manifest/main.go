package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"twitter-clone/internal/releaseplan"
)

func main() {
	root, err := os.Getwd()
	if err == nil {
		err = run(root, os.Args[1:], os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "release evidence stopped:", err)
		os.Exit(1)
	}
}
func run(root string, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("release-manifest", flag.ContinueOnError)
	flags.SetOutput(out)
	assemble := flags.Bool("assemble", false, "assemble exact image/check receipts; does not deploy")
	verify := flags.String("verify-manifest", "", "verify exact image/check/input hashes offline")
	requirePublished := flags.Bool("require-published", false, "reject candidate OCI receipts in verification")
	matrix := flags.Bool("matrix", false, "emit only the GitHub matrix schema")
	bases := flags.Bool("validate-bases", false, "validate bounded build inputs before Docker execution")
	image := flags.String("record-image", "", "record a trusted CI build result")
	check := flags.String("record-check", "", "record a completed CI check")
	source := flags.String("source-sha", "", "exact source commit")
	repository := flags.String("repository", "", "image repository including component name")
	digest := flags.String("digest", "", "actual build output digest")
	published := flags.Bool("published", false, "image was pushed by the trusted CI job")
	builder := flags.String("builder-image", "", "build base image")
	runtime := flags.String("runtime-image", "", "runtime base image")
	evidence := flags.String("evidence-file", "", "bounded local command evidence")
	images := flags.String("receipts-dir", "tmp/release-images", "image receipts directory")
	checks := flags.String("checks-dir", "tmp/release-checks", "check receipts directory")
	output := flags.String("out", "", "exclusive repository-relative output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected release argument")
	}
	modes := 0
	if *verify != "" {
		modes++
	}
	if *matrix {
		modes++
	}
	if *bases {
		modes++
	}
	if *assemble {
		modes++
	}
	if *image != "" {
		modes++
	}
	if *check != "" {
		modes++
	}
	if modes > 1 {
		return errors.New("release modes cannot be combined")
	}
	if *requirePublished && *verify == "" {
		return errors.New("require-published needs verify-manifest")
	}
	if *assemble || *image != "" || *check != "" {
		if err := verifySource(root, *source); err != nil {
			return err
		}
	}
	var value interface{}
	var err error
	switch {
	case *verify != "":
		var manifest releaseplan.Manifest
		manifest, err = releaseplan.Verify(root, *verify, *requirePublished)
		if err == nil {
			err = verifySource(root, manifest.SourceSHA)
		}
		value = manifest
	case *bases:
		err = releaseplan.ValidateBases(*builder, *runtime, *published)
		value = map[string]bool{"validated": err == nil}
	case *assemble:
		value, err = releaseplan.Assemble(root, *source, *images, *checks)
	case *image != "":
		value, err = releaseplan.NewImage(root, releaseplan.ImageReceipt{Service: *image, SourceSHA: *source, Repository: *repository, Digest: *digest, Published: *published, BuilderImage: *builder, RuntimeImage: *runtime})
	case *check != "":
		value, err = releaseplan.NewCheck(root, *check, *source, *evidence)
	default:
		var catalog releaseplan.Catalog
		catalog, _, err = releaseplan.Load(root)
		if *matrix {
			value = struct {
				Include []releaseplan.Component `json:"include"`
			}{catalog.Include}
		} else {
			value = catalog
		}
	}
	if err != nil {
		return err
	}
	if *output != "" {
		return releaseplan.Write(root, *output, value)
	}
	return json.NewEncoder(out).Encode(value)
}

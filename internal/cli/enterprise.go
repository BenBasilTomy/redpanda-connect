// Copyright 2024 Redpanda Data, Inc.
//
// Licensed as a Redpanda Enterprise file under the Redpanda Community
// License (the "License"); you may not use this file except in compliance with
// the License. You may obtain a copy of the License at
//
// https://github.com/redpanda-data/connect/blob/main/licenses/rcl.md

package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/redpanda-data/benthos/v4/public/service"
	"github.com/rs/xid"

	"github.com/redpanda-data/connect/v4/internal/impl/kafka/enterprise"
)

func redpandaTopLevelConfigField() *service.ConfigField {
	return service.NewObjectField("redpanda", enterprise.TopicLoggerFields()...)
}

// Schema returns the config schema for Redpanda Connect.
func Schema(removeDefaultInputOutput bool, version, dateBuilt string) *service.ConfigSchema {
	s := service.NewEnvironment().FullConfigSchema(version, dateBuilt)
	if removeDefaultInputOutput {
		s.SetFieldDefault(map[string]any{}, "input")
		s.SetFieldDefault(map[string]any{}, "output")
	}
	s = s.Field(redpandaTopLevelConfigField())
	return s
}

// InitEnterpriseCLI kicks off the benthos cli with a suite of options that adds
// all of the enterprise functionality of Redpanda Connect. This has been
// abstracted into a separate package so that multiple distributions (classic
// versus cloud) can reference the same code.
func InitEnterpriseCLI(binaryName, version, dateBuilt string, removeDefaultInputOutput bool, opts ...service.CLIOptFunc) {
	rpLogger := enterprise.NewTopicLogger(xid.New().String())
	var fbLogger *service.Logger

	opts = append(opts,
		service.CLIOptSetVersion(version, dateBuilt),
		service.CLIOptSetBinaryName(binaryName),
		service.CLIOptSetProductName("Redpanda Connect"),
		service.CLIOptSetDefaultConfigPaths(
			"redpanda-connect.yaml",
			"/redpanda-connect.yaml",
			"/etc/redpanda-connect/config.yaml",
			"/etc/redpanda-connect.yaml",

			"connect.yaml",
			"/connect.yaml",
			"/etc/connect/config.yaml",
			"/etc/connect.yaml",

			// Keep these for now, for backwards compatibility
			"/benthos.yaml",
			"/etc/benthos/config.yaml",
			"/etc/benthos.yaml",
		),
		service.CLIOptSetDocumentationURL("https://docs.redpanda.com/redpanda-connect"),
		service.CLIOptSetMainSchemaFrom(func() *service.ConfigSchema {
			return Schema(removeDefaultInputOutput, version, dateBuilt)
		}),
		service.CLIOptOnLoggerInit(func(l *service.Logger) {
			fbLogger = l
			rpLogger.SetFallbackLogger(l)
		}),
		service.CLIOptAddTeeLogger(slog.New(rpLogger)),
		service.CLIOptOnConfigParse(func(fn *service.ParsedConfig) error {
			return rpLogger.InitOutputFromParsed(fn.Namespace("redpanda"))
		}),
		service.CLIOptOnStreamStart(func(s *service.RunningStreamSummary) error {
			rpLogger.SetStreamSummary(s)
			return nil
		}),
	)

	exitCode, err := service.RunCLIToCode(context.Background(), opts...)
	if err != nil {
		if fbLogger != nil {
			fbLogger.Error(err.Error())
		} else {
			fmt.Fprintln(os.Stderr, err.Error())
		}
	}
	rpLogger.TriggerEventStopped(err)

	_ = rpLogger.Close(context.Background())
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

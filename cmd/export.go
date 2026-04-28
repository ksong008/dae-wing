/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2023, daeuniverse Organization <team@v2raya.org>
 */

package cmd

import (
	"fmt"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/engine"
	"github.com/daeuniverse/dae-wing/transport/httpapi"
	daeConfig "github.com/daeuniverse/dae/config"
	jsoniter "github.com/json-iterator/go"
	"github.com/spf13/cobra"
)

var (
	exportCmd = &cobra.Command{
		Use:   "export",
		Short: "Export development related information",
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
		},
	}
	exportOutlineCmd = &cobra.Command{
		Use: "outline",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(daeConfig.ExportOutlineJson(db.AppVersion))
		},
	}
	exportOpenAPICmd = &cobra.Command{
		Use: "openapi",
		Run: func(cmd *cobra.Command, args []string) {
			b, _ := jsoniter.MarshalIndent(httpapi.OpenAPIDocument(), "", "  ")
			fmt.Println(string(b))
		},
	}
	exportFlatDescCmd = &cobra.Command{
		Use: "flatdesc",
		Run: func(cmd *cobra.Command, args []string) {
			b, _ := jsoniter.MarshalIndent(map[string]interface{}{
				"Version": db.AppVersion,
				"Desc":    engine.Default().ExportFlatDesc(),
			}, "", "  ")
			fmt.Println(string(b))
		},
	}
)

func init() {
	exportCmd.AddCommand(exportOutlineCmd)
	exportCmd.AddCommand(exportOpenAPICmd)
	exportCmd.AddCommand(exportFlatDescCmd)
}

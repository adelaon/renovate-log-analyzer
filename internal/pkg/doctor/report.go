// Copyright 2025 Red Hat, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package doctor

import (
	"fmt"
	"slices"
	"strings"
)

func (r *SimpleReport) Error(msg string, fields ...interface{}) {
	formatted := formatSimpleMessage(msg, fields)
	r.Errors = append(r.Errors, formatted)
}

func (r *SimpleReport) Warning(msg string, fields ...interface{}) {
	formatted := formatSimpleMessage(msg, fields)
	if slices.Contains(r.Warnings, formatted) {
		return
	}
	r.Warnings = append(r.Warnings, formatted)
}

func (r *SimpleReport) Info(msg string, fields ...interface{}) {
	formatted := formatSimpleMessage(msg, fields)
	r.Infos = append(r.Infos, formatted)
}

func formatSimpleMessage(msg string, fields []interface{}) string {
	if len(fields) == 0 {
		return msg
	}

	var result strings.Builder
	result.WriteString(msg)

	for i := 0; i < len(fields); i += 2 {
		if i+1 < len(fields) {
			key := fmt.Sprintf("%v", fields[i])
			value := fields[i+1]
			if key == "Message" {
				result.WriteString(fmt.Sprintf("\n%s: %v\n", key, value))
			} else {
				result.WriteString(fmt.Sprintf(" | %s: %v", key, value))
			}

		}
	}

	return result.String()
}

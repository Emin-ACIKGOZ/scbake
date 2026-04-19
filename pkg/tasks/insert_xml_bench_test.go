// Copyright 2025 Emin Salih Açíkgöz
// SPDX-License-Identifier: gpl3-or-later

package tasks

import (
	"fmt"
	"strings"
	"testing"
)

// BenchmarkContainsNormalizedXML_Small tests small snippet detection (current O(n²) algorithm)
func BenchmarkContainsNormalizedXML_Small(b *testing.B) {
	fileContent := strings.Repeat(`<plugin><groupId>test</groupId></plugin>
`, 100)
	snippet := `<plugin><groupId>test</groupId></plugin>`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		containsNormalizedXML(fileContent, snippet)
	}
}

// BenchmarkContainsNormalizedXML_Medium tests medium POM files (realistic)
func BenchmarkContainsNormalizedXML_Medium(b *testing.B) {
	fileContent := strings.Repeat(`<plugin>
	<groupId>org.apache.maven.plugins</groupId>
	<artifactId>maven-checkstyle-plugin</artifactId>
	<version>1.0</version>
</plugin>
`, 50)
	snippet := `<plugin>
	<groupId>org.apache.maven.plugins</groupId>
	<artifactId>maven-checkstyle-plugin</artifactId>
	<version>1.0</version>
</plugin>`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		containsNormalizedXML(fileContent, snippet)
	}
}

// BenchmarkContainsNormalizedXML_Large tests large POM files (worst case)
func BenchmarkContainsNormalizedXML_Large(b *testing.B) {
	fileContent := strings.Repeat(`<plugin>
	<groupId>org.apache.maven.plugins</groupId>
	<artifactId>maven-compiler-plugin</artifactId>
	<version>3.8.1</version>
	<configuration>
		<source>11</source>
		<target>11</target>
	</configuration>
</plugin>
`, 500)
	snippet := `<plugin>
	<groupId>org.example</groupId>
	<artifactId>new-plugin</artifactId>
</plugin>`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		containsNormalizedXML(fileContent, snippet)
	}
}

// BenchmarkValidateXML_Small tests XML validation performance
func BenchmarkValidateXML_Small(b *testing.B) {
	xml := `<root><child>test</child></root>`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateXML(xml)
	}
}

// BenchmarkValidateXML_Large tests validation on large documents
func BenchmarkValidateXML_Large(b *testing.B) {
	var sb strings.Builder
	sb.WriteString(`<project>`)
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&sb, `<plugin id="%d"><groupId>test</groupId></plugin>`, i)
	}
	sb.WriteString(`</project>`)
	xml := sb.String()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateXML(xml)
	}
}

// BenchmarkInsertXMLElement_Small tests element insertion performance
func BenchmarkInsertXMLElement_Small(b *testing.B) {
	fileContent := `<project>
	<build>
		<plugins>
		</plugins>
	</build>
</project>`
	xmlToInsert := `<plugin><groupId>test</groupId></plugin>`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = insertXMLElement(fileContent, "/project/build/plugins", xmlToInsert)
	}
}

// BenchmarkInsertXMLElement_Large tests insertion in large files
func BenchmarkInsertXMLElement_Large(b *testing.B) {
	var sb strings.Builder
	sb.WriteString(`<project>
	<build>
		<plugins>`)
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&sb, `
			<plugin>
				<groupId>org.apache.maven.plugins</groupId>
				<artifactId>plugin-%d</artifactId>
			</plugin>`, i)
	}
	sb.WriteString(`
		</plugins>
	</build>
</project>`)
	fileContent := sb.String()
	xmlToInsert := `<plugin><groupId>new</groupId></plugin>`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = insertXMLElement(fileContent, "/project/build/plugins", xmlToInsert)
	}
}

// BenchmarkInsertXMLElement_VeryLarge tests insertion with many plugins (stress test)
func BenchmarkInsertXMLElement_VeryLarge(b *testing.B) {
	var sb strings.Builder
	sb.WriteString(`<project>
	<build>
		<plugins>`)
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&sb, `
			<plugin>
				<groupId>org.example</groupId>
				<artifactId>plugin-%d</artifactId>
				<version>1.0.0</version>
			</plugin>`, i)
	}
	sb.WriteString(`
		</plugins>
	</build>
</project>`)
	fileContent := sb.String()
	xmlToInsert := `<plugin><groupId>new</groupId><artifactId>test</artifactId></plugin>`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = insertXMLElement(fileContent, "/project/build/plugins", xmlToInsert)
	}
}

// BenchmarkContainsNormalizedXML_NotFound tests worst case (element not found)
func BenchmarkContainsNormalizedXML_NotFound(b *testing.B) {
	fileContent := strings.Repeat(`<plugin><groupId>org.example.plugin1</groupId></plugin>
`, 100)
	snippet := `<plugin><groupId>org.example.plugin999</groupId></plugin>`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		containsNormalizedXML(fileContent, snippet)
	}
}

// BenchmarkContainsNormalizedXML_AtEnd tests element at end of file
func BenchmarkContainsNormalizedXML_AtEnd(b *testing.B) {
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		sb.WriteString(`<plugin><groupId>org.example.plugin</groupId></plugin>
`)
	}
	fileContent := sb.String()
	snippet := `<plugin><groupId>org.example.plugin</groupId></plugin>`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		containsNormalizedXML(fileContent, snippet)
	}
}

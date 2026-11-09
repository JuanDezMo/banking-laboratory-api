// Cargador mínimo de archivos .env, solo con la biblioteca estándar.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// loadDotEnv lee líneas KEY=VALUE del archivo path y las define como variables de entorno.
//   - Si el archivo no existe, no hace nada (en Docker las variables llegan por el entorno).
//   - No pisa variables ya definidas: el entorno real tiene prioridad sobre el archivo.
//   - Ignora líneas vacías y comentarios (#). Quita comillas simples o dobles del valor.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for lineNum := 1; scanner.Scan(); lineNum++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			return fmt.Errorf("%s:%d: expected KEY=VALUE", path, lineNum)
		}
		value = unquote(strings.TrimSpace(value))

		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// unquote quita un par de comillas que envuelvan el valor completo: "x" o 'x'.
func unquote(value string) string {
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

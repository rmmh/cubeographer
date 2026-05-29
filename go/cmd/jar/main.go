package main

import (
	"archive/zip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/rmmh/cubeographer/go/jvm"
	rp "github.com/rmmh/cubeographer/go/resourcepack"
)

func main() {
	versionFlag := flag.String("version", "26.1.2", "Minecraft version to work with")
	outFlag := flag.String("out", "dist", "Output directory")
	dumpStringsFlag := flag.Bool("dump-strings", false, "Dump all string constants in each class")
	findWaterloggableFlag := flag.Bool("find-waterloggable", false, "Find all waterloggable blocks dynamically")

	flag.Parse()

	// Ensure output directory exists
	err := os.MkdirAll(*outFlag, 0755)
	if err != nil {
		log.Fatalf("failed to create output directory: %v", err)
	}

	jarName := fmt.Sprintf("client_%s.jar", *versionFlag)
	jarPath := filepath.Join(*outFlag, jarName)

	// Check if jar exists, download if not
	if _, err := os.Stat(jarPath); os.IsNotExist(err) {
		fmt.Printf("Jar not present at %s, downloading Minecraft version %s...\n", jarPath, *versionFlag)
		err = rp.DownloadMinecraftJar(jarPath, *versionFlag)
		if err != nil {
			log.Fatalf("failed to download Minecraft jar: %v", err)
		}
	} else {
		fmt.Printf("Using existing jar at %s\n", jarPath)
	}

	if *dumpStringsFlag {
		fmt.Println("Dumping strings from classes...")
		err = dumpStrings(jarPath, *outFlag)
		if err != nil {
			log.Fatalf("failed to dump strings: %v", err)
		}
	}

	if *findWaterloggableFlag {
		fmt.Println("Finding waterloggable blocks...")
		err = findWaterloggable(jarPath, *outFlag)
		if err != nil {
			log.Fatalf("failed to find waterloggable blocks: %v", err)
		}
	}
}

func dumpStrings(jarPath string, outDir string) error {
	jar, err := zip.OpenReader(jarPath)
	if err != nil {
		return fmt.Errorf("unable to open jar %s: %v", jarPath, err)
	}
	defer jar.Close()

	// Map of class name to list of strings
	classStrings := make(map[string][]string)

	for _, f := range jar.File {
		if filepath.Ext(f.Name) == ".class" {
			rc, err := f.Open()
			if err != nil {
				return fmt.Errorf("failed to open class file %s: %v", f.Name, err)
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return fmt.Errorf("failed to read class file %s: %v", f.Name, err)
			}

			cf, err := jvm.ParseClassFile(data)
			if err != nil {
				// Skip classes we fail to parse (e.g. invalid class format or obfuscation issues)
				continue
			}

			var stringsInClass []string
			seen := make(map[string]bool)

			// Collect all CONSTANT_String constants
			for i := 1; i < len(cf.ConstantPool); i++ {
				if s, ok := cf.ConstantPool[i].(jvm.CPString); ok {
					strVal := cf.ResolveUtf8(s.StringIndex)
					if strVal != "" && !seen[strVal] {
						seen[strVal] = true
						stringsInClass = append(stringsInClass, strVal)
					}
				}
			}

			if len(stringsInClass) > 0 {
				classStrings[cf.ThisClass] = stringsInClass
			}
		}
	}

	// Write out to JSON file
	jsonPath := filepath.Join(outDir, "strings.json")
	jsonFile, err := os.Create(jsonPath)
	if err != nil {
		return fmt.Errorf("failed to create strings.json: %v", err)
	}
	defer jsonFile.Close()

	encoder := json.NewEncoder(jsonFile)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(classStrings)
	if err != nil {
		return fmt.Errorf("failed to encode strings.json: %v", err)
	}

	fmt.Printf("Dumped strings of %d classes to %s\n", len(classStrings), jsonPath)
	return nil
}

func findWaterloggable(jarPath string, outDir string) error {
	blocksClass, blockClass, registerMethod, _, err := DetectRegistryMeta(jarPath)
	if err != nil {
		return fmt.Errorf("failed auto-detection of registry metadata: %v", err)
	}

	jar, err := zip.OpenReader(jarPath)
	if err != nil {
		return fmt.Errorf("unable to open jar %s: %v", jarPath, err)
	}
	defer jar.Close()

	provider := jvm.NewZipClassProvider(jar)
	interp := jvm.NewInterpreter(provider)
	interp.BlocksClassName = blocksClass
	interp.BlockClassName = blockClass
	interp.RegisterMethodName = registerMethod

	fmt.Printf("Interpreting block registry static initializer for class %s...\n", blocksClass)
	err = interp.InterpretClassClinit(blocksClass)
	if err != nil {
		return fmt.Errorf("failed to interpret block registry: %v", err)
	}

	fmt.Println("Discovering waterloggable blocks dynamically using library...")
	waterloggableBlocks, err := rp.FindWaterloggableBlocks(provider, interp.Blocks)
	if err != nil {
		return fmt.Errorf("failed to find waterloggable blocks: %v", err)
	}

	// Save to JSON
	jsonPath := filepath.Join(outDir, "waterloggable_blocks.json")
	jsonFile, err := os.Create(jsonPath)
	if err != nil {
		return fmt.Errorf("failed to create waterloggable_blocks.json: %v", err)
	}
	defer jsonFile.Close()

	encoder := json.NewEncoder(jsonFile)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(waterloggableBlocks)
	if err != nil {
		return fmt.Errorf("failed to encode waterloggable_blocks.json: %v", err)
	}

	fmt.Printf("Successfully identified %d waterloggable blocks!\n", len(waterloggableBlocks))
	fmt.Printf("List saved to %s\n", jsonPath)
	return nil
}

func DetectRegistryMeta(jarPath string) (blocksClass, blockClass, registerMethod, initMethod string, err error) {
	jar, err := zip.OpenReader(jarPath)
	if err != nil {
		return "", "", "", "", fmt.Errorf("unable to open jar %s: %v", jarPath, err)
	}
	defer jar.Close()

	blocksClass, blockClass, registerMethod, initMethod, err = rp.DetectRegistryMeta(jar)
	if err != nil {
		return "", "", "", "", err
	}

	fmt.Printf("Auto-discovered register class: %q\n", blocksClass)
	fmt.Printf("Auto-discovered Block class: %q\n", blockClass)
	fmt.Printf("Auto-discovered register method: %q\n", registerMethod)
	fmt.Printf("Auto-discovered initialization method: %q\n", initMethod)

	return blocksClass, blockClass, registerMethod, initMethod, nil
}

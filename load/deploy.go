package load

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

var yamlNodeType = reflect.TypeOf(yaml.Node{})

// open is overriden in tests.
var open = os.Open

// A Spec represents a structure that yaml can be decoded into.  The type is the
// type of structure to decode the yaml into.  As an example,
// reflect.TypeOf(KindSpec{}).
//
// The Validate function is called once the yaml file has been decoded into a
// new created structure of type Type.
type Spec struct {
	Type     interface{}
	Tag      string // Associated "kne" tag in the used in the deployment structure
	Validate func(c *Config, node interface{}) error
}

var (
	specs  = map[string]*Spec{}
	specMu sync.Mutex
)

// Register registers kind with the provided spec.
func Register(kind string, spec *Spec) {
	specMu.Lock()
	specs[kind] = spec
	specMu.Unlock()
}

// A Config represents a KNE deployment configuration.
type Config struct {
	Path       string      // Path of the configuration file
	Dir        string      // Absolute path of the diretory Path is in
	Config     interface{} // The configuration structure
	Deployment interface{} // Filled by Config.Decode

	IgnoreMissingFiles bool // when set there is no error when a file is missing.
}

// NewConfig reads a yaml configuration file at path and populates the provided
// config structure.
//
// A sample config structure:
//
//	type DeploymentConfig struct {
//		Cluster     ClusterSpec       `yaml:"cluster"`
//		Ingress     IngressSpec       `yaml:"ingress"`
//		CNI         CNISpec           `yaml:"cni"`
//		Controllers []*ControllerSpec `yaml:"controllers"`
//	}
func NewConfig(path string, config interface{}) (*Config, error) {
	c := &Config{
		Path:   path,
		Config: config,
	}
	var err error
	if c.Dir, err = filepath.Abs(path); err != nil {
		return nil, err
	}
	c.Dir = filepath.Dir(c.Dir)
	fp, err := open(path)
	if err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(fp)
	decoder.KnownFields(true)
	if err := decoder.Decode(config); err != nil {
		return nil, err
	}
	return c, nil
}

// Decode decodes the configuration in Config c and populates deployment. Only
// fields in the deployment structure that have a registered KNE tag are
// populated.  These fields are populated from configuration nodes that have a
// "kind" and a "spec" field that match a previously registered spec.
//
// An example deployment structure:
//
//	type Deployment struct {
//		Cluster     Cluster      `kne:"cluster"`
//		Ingress     Ingress      `kne:"ingress"`
//		CNI         CNI          `kne:"cni"`
//		Controllers []Controller `kne:"controllers"`
//	}
func (c *Config) Decode(deployment interface{}) error {
	c.Deployment = deployment
	err := c.decode(reflect.ValueOf(c.Config), []string{"root"}, "")
	return err
}

func (c *Config) decode(v reflect.Value, path []string, tag reflect.StructTag) (rerr error) {
	t := v.Type()
	if t == yamlNodeType {
		yn := v.Interface().(yaml.Node)
		var data []byte
		if err := yn.Encode(&data); err != nil {
			return err
		}
	}
	switch v.Kind() {
	case reflect.String:
		if v.String() == "" {
			return nil
		}
		if tag.Get("kne") == "yaml" {
			if err := c.checkYAMLFile(v); err != nil {
				return fmt.Errorf("%s: %v", strings.Join(path, "."), err)
			}
		}
	case reflect.Array:
		n := v.Len()
		for i := 0; i < n; i++ {
			if err := c.decode(v.Index(i), append(path, fmt.Sprintf("[%d]", i)), tag); err != nil {
				return err
			}
		}
	case reflect.Map:
		itr := v.MapRange()
		for itr.Next() {
			key := itr.Key()
			val := itr.Value()
			if key.Kind() == reflect.String {
				if err := c.decode(val, append(path, fmt.Sprintf("[%s]", key.String())), tag); err != nil {
					return err
				}
			}
		}
	case reflect.Pointer:
		if err := c.decode(v.Elem(), path, tag); err != nil {
			return err
		}
	case reflect.Slice:
		n := v.Len()
		for i := 0; i < n; i++ {
			if err := c.decode(v.Index(i), append(path, fmt.Sprintf("[%d]", i)), tag); err != nil {
				return err
			}
		}
	case reflect.Struct:
		// Decode all the struct fields while looking for a structure
		// that has both a kind and a spec YAML field.  We expect the
		// spec field to be a yaml.Node which should be decoded based on
		// kind.  The newly created and decoded structure is added to
		// the Deployment field that has the same kne tag that was
		// registered with the spec by Register.  We will fail on a
		// structure that has one but not both of these fields.
		n := t.NumField()
		kind := ""
		var spec *yaml.Node
		for i := 0; i < n; i++ {
			sf := t.Field(i)
			sv := v.Field(i)

			switch sf.Tag.Get("yaml") {
			case "kind":
				if kind != "" {
					return fmt.Errorf("multiple kinds in %v", t)
				}
				if sf.Type.Kind() != reflect.String {
					return fmt.Errorf("kind must be a string")
				}
				kind = sv.String()
				if err := c.decode(sv, append(path, sf.Name), sf.Tag); err != nil {
					return err
				}
			case "spec":
				if sf.Type != yamlNodeType {
					return fmt.Errorf("%s is not of type %v\n", strings.Join(append(path, sf.Name), "."), yamlNodeType)
				}
				node := sv.Interface().(yaml.Node)
				spec = &node
			case "":
			default:
				if err := c.decode(sv, append(path, sf.Name), sf.Tag); err != nil {
					return err
				}
			}
		}
		switch {
		case kind == "" && spec == nil:
		case kind == "":
			return fmt.Errorf("spec field without kind: %s\n", strings.Join(path, "."))
		case spec == nil:
			return fmt.Errorf("kind field without spec: %s\n", strings.Join(path, "."))
		default:
			// kind and spec have been supplied.

			st, ok := specs[kind]
			if !ok {
				return fmt.Errorf("kind %s not supported", kind)
			}

			// Create a structure of the correct type and decode the
			// yaml.Node into it.
			val := reflect.New(reflect.ValueOf(st.Type).Type())
			if err := spec.Decode(val.Interface()); err != nil {
				return err
			}
			if err := c.decode(val.Elem(), append(path, kind), ""); err != nil {
				return err
			}

			// Find the field in the provided deployment structure
			// that has the kne tag associated with the spec.
			fv, err := findField(c.Deployment, st.Tag)
			if err != nil {
				return err
			}
			t := fv.Type()

			// If our deployment field is a slice then validate
			// against the slice's element type.
			isSlice := t.Kind() == reflect.Slice
			if isSlice {
				t = t.Elem()
			}

			if !val.Type().AssignableTo(t) {
				fmt.Printf("%v is not assignable to %v\n", val.Type(), t)
				return fmt.Errorf("%v is not assignable to %v", val.Type(), t)
			}

			if isSlice {
				fv.Set(reflect.Append(fv, val))
			} else {
				fv.Set(val)
			}

			if st.Validate != nil {
				if err := st.Validate(c, val.Interface()); err != nil {
					return err
				}
			}
		}

	// Scalar types we currently ignore.
	case reflect.Bool:
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
	case reflect.Float32, reflect.Float64:
	case reflect.Complex64, reflect.Complex128:

	// We always ignore these
	case reflect.Chan:
	case reflect.Func:
	case reflect.Interface:
	case reflect.UnsafePointer:
	default:
		fmt.Fprintf(os.Stderr, "Unsupported value type %v\n", v.Kind())
	}
	return nil
}

// checkYAMLFile returns an error if v is not a string, is not a path to an existing
// file, or the file cannot be decoded as a YAML file.
func (c *Config) checkYAMLFile(v reflect.Value) error {
	if v.Kind() != reflect.String {
		return fmt.Errorf("is of type %v, not string", v.Kind())
	}
	path := v.String()
	if !filepath.IsAbs(path) {
		path = filepath.Join(c.Dir, path)
	}
	v.SetString(path)
	fi, err := os.Stat(path)
	if err != nil {
		if c.IgnoreMissingFiles {
			return nil
		}
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s: not a regular file", path)
	}
	fp, err := open(path)
	if err != nil {
		return err
	}
	defer fp.Close()

	decoder := yaml.NewDecoder(fp)
	if err := decoder.Decode(map[interface{}]interface{}{}); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// findField returns the reflect.Value of the first field in s that has the kne
// struct tag of tag.  The type of s must be a pointer to struct. An error is
// return is no field is found or s is not a pointer to a structure.
func findField(s interface{}, tag string) (reflect.Value, error) {
	t := reflect.TypeOf(s)
	if t.Kind() != reflect.Pointer || t.Elem().Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("%T is not a pointer to struct", s)
	}
	t = t.Elem()

	n := t.NumField()
	for i := 0; i < n; i++ {
		sf := t.Field(i)
		if sf.Tag.Get("kne") == tag {
			return reflect.ValueOf(s).Elem().Field(i), nil
		}
	}
	return reflect.Value{}, fmt.Errorf("field %s not found", tag)
}

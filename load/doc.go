// Package load reads KNE deployment configurations, populates a deployment
// structure, and does basic validation on the configuration.  It requires
// yaml tags to be defined for all struct fields specified in the YAML
// configuration.
//
// Fields with the kne tag "yaml" (kne:"yaml") are interpreted as path names to
// YAML files.  An error is generated if the file does not exist or cannot be
// parsed as a YAML file.
//
// Nodes that have the "kind" and "spec" yaml tags expect the spec field to be a
// *yaml.Node and the kind has been registered with Register.
//
// Typical Usage:
//
//	var config DeploymentConfig{}
//	var deployment Deployment{}
//	c, err := NewConfig(file, &config)
//	if err != nil {
//		...
//	}
//	if err := c.Decode(&deployment); err != nil {
//		...
//	}
//	// deployment is now ready for use.
package load

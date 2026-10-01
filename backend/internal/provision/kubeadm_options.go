package provision

import (
	"errors"
	"fmt"
)

// NormalizeKubeadmOptions fills in defaults from cfg and validates what a user
// asked for: the Kubernetes minor and CNI must be ones this Vinaire is
// configured to install, and the free-form fields must have a safe shape.
func NormalizeKubeadmOptions(cfg KubeadmConfig, o KubeadmOptions) (KubeadmOptions, error) {
	if o.Minor == "" {
		o.Minor = cfg.DefaultMinor()
	}
	if !cfg.HasMinor(o.Minor) {
		return o, fmt.Errorf("Kubernetes %q isn't in this Vinaire's supported list", o.Minor)
	}
	cni, ok := cfg.CNI(o.CNI)
	if !ok {
		return o, fmt.Errorf("unknown pod network %q", o.CNI)
	}
	o.CNI = cni.Name
	if o.ControlPlaneEndpoint != "" && !endpointRe.MatchString(o.ControlPlaneEndpoint) {
		return o, errors.New("the control-plane endpoint must be a host name or IP, optionally with :port")
	}
	if len(o.IgnorePreflightErrors) > 10 {
		return o, errors.New("too many preflight checks to ignore")
	}
	for _, n := range o.IgnorePreflightErrors {
		if !preflightNameRe.MatchString(n) {
			return o, fmt.Errorf("%q isn't a valid kubeadm preflight check name", n)
		}
	}
	return o, nil
}

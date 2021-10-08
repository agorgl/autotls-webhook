package main

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	admissionv1 "k8s.io/api/admission/v1"
	networking "k8s.io/api/networking/v1"
	"log"
	"os"
	"strings"
)

type JSONPatchEntry struct {
	OP    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value,omitempty"`
}

func handleMutate(c *gin.Context) {
	// Deserialize request
	admissionReview := &admissionv1.AdmissionReview{}
	if err := c.Bind(admissionReview); err != nil {
		log.Println("Error deserializing admission review:", err)
		return
	}

	// Default, passthrough response
	admissionResponse := &admissionv1.AdmissionResponse{}
	admissionResponse.UID = admissionReview.Request.UID
	admissionResponse.Allowed = true
	response := &admissionv1.AdmissionReview{}
	response.Response = admissionResponse
	response.SetGroupVersionKind(admissionReview.GroupVersionKind())

	// Filter out unwanted requests
	admissionRequest := admissionReview.Request
	if !(admissionRequest.Kind.Kind == "Ingress" && admissionRequest.Operation == admissionv1.Create) {
		log.Println("Admission request with kind", admissionRequest.Kind.Kind, "and operation", admissionRequest.Operation, "skipping")
		c.JSON(200, response)
		return
	}

	// Deserialize ingress object
	ingress := &networking.Ingress{}
	if err := json.Unmarshal(admissionReview.Request.Object.Raw, ingress); err != nil {
		log.Println("Error deserializing ingress object from admission review:", err)
		c.JSON(200, response)
		return
	}

	// Skip ingresses that have already a cluster-issuer defined
	if issuer, ok := ingress.Annotations["cert-manager.io/cluster-issuer"]; ok {
		log.Println("Issuer", issuer, "for", ingress.Name, "ingress already specified, skipping automatic setup")
		c.JSON(200, response)
		return
	}

	// Expand unqualified hosts
	specChanged := false
	ingressDomain := os.Getenv("INGRESS_DOMAIN")
	if ingressDomain != "" {
		for idx := range ingress.Spec.Rules {
			rule := &ingress.Spec.Rules[idx]
			if contains := strings.Contains(rule.Host, "."); !contains {
				rule.Host = rule.Host + "." + ingressDomain
				specChanged = true
			}
		}
	}

	// Add cluster issuer
	annotationsChanged := false
	clusterIssuer := os.Getenv("CLUSTER_ISSUER")
	if clusterIssuer != "" {
		annotationsChanged = true
		ingress.Annotations["cert-manager.io/cluster-issuer"] = clusterIssuer
	}

	// Gather hosts list
	hosts := []string{}
	for _, r := range ingress.Spec.Rules {
		if len(r.Host) > 0 {
			hosts = append(hosts, r.Host)
		}
	}

	// Add tls configuration if missing
	if len(ingress.Spec.TLS) == 0 {
		specChanged = true
		ingress.Spec.TLS = []networking.IngressTLS{
			{
				Hosts:      hosts,
				SecretName: ingress.Name + "-tls",
			},
		}
	}

	// Create specPatch if spec object changed
	var specPatch *JSONPatchEntry
	if specChanged {
		bytes, err := json.Marshal(ingress.Spec)
		if err == nil {
			specPatch = &JSONPatchEntry{
				OP:    "replace",
				Path:  "/spec",
				Value: bytes,
			}
		} else {
			log.Println("Could not serialize spec:", err)
		}
	}

	// Create annotationsPatch if annotations list changed
	var annotationsPatch *JSONPatchEntry
	if annotationsChanged {
		bytes, err := json.Marshal(ingress.ObjectMeta.Annotations)
		if err == nil {
			annotationsPatch = &JSONPatchEntry{
				OP:    "replace",
				Path:  "/metadata/annotations",
				Value: bytes,
			}
		} else {
			log.Println("Could not serialize annotations:", err)
		}
	}

	// Append non-nil patch entries
	patch := []JSONPatchEntry{}
	if specPatch != nil {
		patch = append(patch, *specPatch)
	}
	if annotationsPatch != nil {
		patch = append(patch, *annotationsPatch)
	}

	// Add patch to response if non empty
	if len(patch) > 0 {
		log.Println("Applying", len(patch), "patches in response")
		patchBytes, err := json.Marshal(patch)
		if err == nil {
			patchType := admissionv1.PatchTypeJSONPatch
			admissionResponse.Patch = patchBytes
			admissionResponse.PatchType = &patchType
		} else {
			log.Println("Could not serialize patch:", err)
		}
	}

	// Send back response
	c.JSON(200, response)
}

func main() {
	r := gin.Default()
	r.POST("/mutate", handleMutate)
	r.RunTLS(":8080", "/tls/tls.crt", "/tls/tls.key")
}

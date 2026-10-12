package main

import (
	"context"
	"errors"

	authzv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/f1nn17/vinaire/backend/internal/authn"
	"github.com/f1nn17/vinaire/backend/internal/clusters"
)

// tokenValidator asks the default cluster whether a pasted token is accepted, by
// making the cheapest call any authenticated identity may make: a
// SelfSubjectAccessReview for `get /version`. A 401 means the token is not valid
// (authn.ErrInvalidToken); any other failure means the cluster couldn't be asked.
// The token is never put in an error message.
func tokenValidator(registry *clusters.Registry) authn.TokenValidator {
	return func(ctx context.Context, token string) error {
		entry := registry.Default()
		if entry == nil || entry.RESTConfig == nil {
			return errors.New("no cluster to validate the token against")
		}
		cs, err := entry.ClientsetForToken(token)
		if err != nil {
			return errors.New("couldn't build a client for the cluster")
		}
		_, err = cs.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authzv1.SelfSubjectAccessReview{
			Spec: authzv1.SelfSubjectAccessReviewSpec{
				NonResourceAttributes: &authzv1.NonResourceAttributes{Path: "/version", Verb: "get"},
			},
		}, metav1.CreateOptions{})
		switch {
		case err == nil:
			return nil
		case apierrors.IsUnauthorized(err):
			return authn.ErrInvalidToken
		default:
			return errors.New("the cluster couldn't be reached")
		}
	}
}

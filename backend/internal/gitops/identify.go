package gitops

import (
	"context"
	"fmt"

	authnv1 "k8s.io/api/authentication/v1"
	authzv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/shiraku/vinaire/backend/internal/clusters"
)

// whoAmI asks the cluster who a token is and whether that identity is allowed to
// do everything. It is how a source's "applies as" line is filled in, and how a
// token the cluster doesn't accept is caught when the source is created rather
// than at the first sync.
func whoAmI(ctx context.Context, entry *clusters.Entry, token string) (identity string, clusterAdmin bool, err error) {
	cs, err := entry.ClientsetForToken(token)
	if err != nil {
		return "", false, err
	}
	identity = "unknown"
	rev, err := cs.AuthenticationV1().SelfSubjectReviews().Create(ctx, &authnv1.SelfSubjectReview{}, metav1.CreateOptions{})
	switch {
	case err == nil:
		identity = rev.Status.UserInfo.Username
	case apierrors.IsUnauthorized(err):
		return "", false, fmt.Errorf("unauthorized")
	case apierrors.IsNotFound(err):
		// an older cluster without the SelfSubjectReview API: the check below
		// still proves the token is accepted
	default:
		return "", false, err
	}
	ssar, err := cs.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authzv1.SelfSubjectAccessReview{
		Spec: authzv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authzv1.ResourceAttributes{Verb: "*", Group: "*", Resource: "*"}},
	}, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsUnauthorized(err) {
			return "", false, fmt.Errorf("unauthorized")
		}
		return identity, false, nil // can't tell; not worth failing the source over
	}
	return identity, ssar.Status.Allowed, nil
}

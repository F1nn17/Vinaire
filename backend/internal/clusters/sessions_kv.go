package clusters

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// sessionsSecretName is the Secret the shared session store lives in (Roadmap Phase
// 48): Vinaire's own namespace, written with its own identity, like the registry.
const sessionsSecretName = "vinaire-sessions"

// SessionBackend is the Secret-backed place for shared sessions. One key per
// session, changed with a merge patch of that key alone, so replicas never overwrite
// each other. Values arrive already sealed.
type SessionBackend struct{ s *SecretStore }

// SessionBackend returns the shared-session backend, and the cipher that seals it.
// ok is false when the store has no encryption key: a session holds a Kubernetes
// token, and it is never written unsealed.
func (s *SecretStore) SessionBackend() (b *SessionBackend, sealer *Cipher, ok bool) {
	if s.cipher == nil {
		return nil, nil, false
	}
	return &SessionBackend{s: s}, s.cipher, true
}

func (b *SessionBackend) Snapshot(ctx context.Context) (map[string][]byte, error) {
	sec, err := b.s.clientset.CoreV1().Secrets(b.s.namespace).Get(ctx, sessionsSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return map[string][]byte{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get %s secret: %w", sessionsSecretName, err)
	}
	return sec.Data, nil
}

func (b *SessionBackend) patch(ctx context.Context, key string, value any) error {
	body, err := json.Marshal(map[string]any{"data": map[string]any{key: value}})
	if err != nil {
		return err
	}
	_, err = b.s.clientset.CoreV1().Secrets(b.s.namespace).Patch(ctx, sessionsSecretName, types.MergePatchType, body, metav1.PatchOptions{})
	return err
}

// Put stores one sealed session (creating the Secret on first use).
func (b *SessionBackend) Put(ctx context.Context, key string, sealed []byte) error {
	err := b.patch(ctx, key, sealed) // []byte marshals as base64, which is what Secret data wants
	if apierrors.IsNotFound(err) {
		_, cerr := b.s.clientset.CoreV1().Secrets(b.s.namespace).Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: sessionsSecretName, Namespace: b.s.namespace},
			Data:       map[string][]byte{key: sealed},
		}, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(cerr) { // another replica created it first
			return b.patch(ctx, key, sealed)
		}
		return cerr
	}
	return err
}

// Remove deletes one session; a Secret that isn't there is not an error.
func (b *SessionBackend) Remove(ctx context.Context, key string) error {
	err := b.patch(ctx, key, nil)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

package contentstaging

import (
	"context"
	"io"

	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
)

type WriteResult struct {
	Size   int64
	Digest string
}

// Provider is the private persistence port below Staging Manager. Physical
// paths, credentials and provider identities never cross the public protocol.
type Provider interface {
	LoadUploads(context.Context) ([]uploadRecord, error)
	LoadProtections(context.Context) ([]protectionRecord, error)
	SaveUpload(context.Context, uploadRecord) error
	SaveProtection(context.Context, protectionRecord) error
	DeleteUpload(context.Context, Scope, string) error
	DeleteProtection(context.Context, Scope, string) error
	WriteStaged(context.Context, Scope, string, int64, io.Reader) (WriteResult, error)
	OpenStaged(context.Context, Scope, string) (io.ReadCloser, error)
	Promote(context.Context, Scope, string, stagingv1.ContentDescriptor) error
	RemoveStaged(context.Context, Scope, string) error
	OpenContent(context.Context, Scope, stagingv1.ContentDescriptor) (io.ReadCloser, error)
	VerifyContent(context.Context, Scope, stagingv1.ContentDescriptor) error
	RemoveContent(context.Context, Scope, string) error
}

type AdmissionRequest struct {
	Scope  Scope
	Upload stagingv1.UploadLease
}

// Admission is intentionally required. P2.4b supplies an integrity policy;
// malware and enterprise DLP providers can replace it without changing Manager.
type Admission interface {
	Admit(context.Context, AdmissionRequest, io.Reader) error
}

// IntegrityAdmission proves the object can be read through the bounded trusted
// path. Digest, size and declaration checks are performed by Manager itself.
// It is not a malware scanner; production content scanning is a later Provider.
type IntegrityAdmission struct{}

func (IntegrityAdmission) Admit(ctx context.Context, _ AdmissionRequest, reader io.Reader) error {
	_, err := io.Copy(io.Discard, contextReader{ctx: ctx, reader: reader})
	return err
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

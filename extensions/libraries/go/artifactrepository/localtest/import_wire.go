package localtest

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"strings"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifacttrust"
)

const maxImportBindingBytes = int64(64 << 10)

func writeImport(writer *multipart.Writer, source artifactrepositoryv1.Profile, receipt artifactrepositoryv1.Receipt, envelope artifacttrust.Envelope) error {
	profileRaw, err := json.Marshal(source)
	if err != nil {
		return err
	}
	receiptRaw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	if err := writeEnvelopePart(writer, "source-profile", "application/json", profileRaw); err != nil {
		return err
	}
	if err := writeEnvelopePart(writer, "source-receipt", "application/json", receiptRaw); err != nil {
		return err
	}
	return writeEnvelope(writer, envelope)
}

func readImport(body io.Reader, contentType string) (artifactrepositoryv1.Profile, artifactrepositoryv1.Receipt, artifacttrust.Envelope, error) {
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") || parameters["boundary"] == "" {
		return artifactrepositoryv1.Profile{}, artifactrepositoryv1.Receipt{}, artifacttrust.Envelope{}, errors.New("local-test 导入必须使用 multipart/form-data")
	}
	values, err := readMultipartValues(multipart.NewReader(body, parameters["boundary"]), map[string]int64{
		"source-profile": maxImportBindingBytes,
		"source-receipt": maxImportBindingBytes,
	})
	if err != nil {
		return artifactrepositoryv1.Profile{}, artifactrepositoryv1.Receipt{}, artifacttrust.Envelope{}, err
	}
	if len(values["source-profile"]) == 0 || len(values["source-receipt"]) == 0 {
		return artifactrepositoryv1.Profile{}, artifactrepositoryv1.Receipt{}, artifacttrust.Envelope{}, errors.New("local-test 导入缺少远端 Profile 或回执")
	}
	profile, err := artifactrepositoryv1.ParseProfile(values["source-profile"])
	if err != nil {
		return artifactrepositoryv1.Profile{}, artifactrepositoryv1.Receipt{}, artifacttrust.Envelope{}, err
	}
	var receipt artifactrepositoryv1.Receipt
	if err := decodeJSONBytes(values["source-receipt"], &receipt); err != nil {
		return artifactrepositoryv1.Profile{}, artifactrepositoryv1.Receipt{}, artifacttrust.Envelope{}, err
	}
	envelope, err := envelopeFromValues(values)
	return profile, receipt, envelope, err
}

func decodeJSONBytes(raw []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("local-test JSON 只能包含一个文档")
	}
	return nil
}

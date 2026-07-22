import type { PlatformAdminClient } from "@vastplan/platform-admin";
import {
  jsonSchemaDialect,
  message,
  type WorkbenchFormDefinition,
} from "@vastplan/workbench-sdk";
import { namespace, type ProviderRow } from "./model.js";

export function providerForms(
  client: PlatformAdminClient,
  generation: () => number,
): WorkbenchFormDefinition<ProviderRow>[] {
  return [
    createForm(client, generation),
    publishForm(client, generation),
  ];
}

function createForm(
  client: PlatformAdminClient,
  generation: () => number,
): WorkbenchFormDefinition<ProviderRow> {
  return {
    id: "create",
    schema: {
      id: "authentication-provider-profile.v1",
      schema: {
        $schema: jsonSchemaDialect,
        type: "object",
        additionalProperties: false,
        required: [
          "id",
          "contributionId",
          "configurationId",
          "configurationRevision",
          "configurationDigest",
          "method",
          "subjectNamespace",
        ],
        properties: {
          id: { type: "string", title: "Profile ID" },
          contributionId: { type: "string", title: "Contribution ID" },
          configurationId: { type: "string", title: "Configuration ID" },
          configurationRevision: {
            type: "integer",
            title: "Configuration revision",
            minimum: 1,
          },
          configurationDigest: {
            type: "string",
            title: "Configuration digest",
            pattern: "^[a-f0-9]{64}$",
          },
          method: { type: "string", title: "Method ID" },
          subjectNamespace: { type: "string", title: "Subject namespace" },
        },
      },
    },
    presentation: {
      layout: "vertical",
      navigation: "sections",
      sections: [
        {
          id: "identity",
          title: message(namespace, "form.identity", "Provider 身份与配置引用"),
          columns: 2,
          fields: [
            "/id",
            "/contributionId",
            "/configurationId",
            "/configurationRevision",
            "/configurationDigest",
            "/method",
            "/subjectNamespace",
          ],
        },
      ],
      fields: [
        { pointer: "/configurationDigest", span: 2 },
        { pointer: "/subjectNamespace", span: 2 },
      ],
    },
    workflow: {
      surface: "drawer",
      title: message(namespace, "create.title", "创建 Provider 草稿"),
      size: "lg",
      submitLabel: message(namespace, "create.submit", "创建草稿"),
      success: {
        refreshCollection: true,
        close: true,
        notify: message(namespace, "create.success", "Provider 草稿已创建"),
      },
    },
    async submit({ value }) {
      await client.createAuthenticationProviderDraft(generation(), {
        version: 1,
        revision: 1,
        id: String(value.id),
        contributionId: String(value.contributionId),
        configuration: {
          id: String(value.configurationId),
          revision: Number(value.configurationRevision),
          digest: String(value.configurationDigest),
        },
        purposes: ["portal-login"],
        methods: [String(value.method)],
        subjectNamespace: String(value.subjectNamespace),
        requiredCapabilities: [],
      });
    },
  };
}

function publishForm(
  client: PlatformAdminClient,
  generation: () => number,
): WorkbenchFormDefinition<ProviderRow> {
  return {
    id: "publish",
    schema: {
      id: "authentication-provider-publication.v1",
      schema: {
        $schema: jsonSchemaDialect,
        type: "object",
        additionalProperties: false,
        required: ["catalogId", "catalogRevision", "bindings", "accessCatalog"],
        properties: {
          catalogId: { type: "string", title: "Catalog ID" },
          catalogRevision: {
            type: "integer",
            title: "Catalog revision",
            minimum: 1,
          },
          bindings: { type: "string", title: "Provider bindings JSON" },
          accessCatalog: {
            type: "string",
            title: "Access Profile Catalog JSON",
          },
        },
      },
    },
    presentation: {
      layout: "vertical",
      navigation: "sections",
      sections: [
        {
          id: "publication",
          title: message(namespace, "publish.section", "原子发布"),
          columns: 1,
          fields: [
            "/catalogId",
            "/catalogRevision",
            "/bindings",
            "/accessCatalog",
          ],
        },
      ],
      fields: [
        { pointer: "/bindings", widget: "textarea" },
        { pointer: "/accessCatalog", widget: "textarea" },
      ],
    },
    workflow: {
      surface: "drawer",
      title: message(
        namespace,
        "publish.title",
        "发布 Provider 与 Access Catalog",
      ),
      submitLabel: message(namespace, "publish.submit", "原子发布"),
      confirmBeforeSubmit: message(
        namespace,
        "publish.confirm",
        "确认同时发布 Provider Binding 与会话前 Access Profile？",
      ),
      success: { refreshCollection: true, close: true },
    },
    async submit({ value }) {
      try {
        await client.publishAuthenticationProviders({
          expectedGeneration: generation(),
          catalogId: String(value.catalogId),
          catalogRevision: Number(value.catalogRevision),
          bindings: JSON.parse(String(value.bindings)),
          accessCatalog: JSON.parse(String(value.accessCatalog)),
        });
      } catch (error) {
        if (error instanceof SyntaxError)
          return {
            fieldErrors: {
              bindings: message(
                namespace,
                "publish.invalid",
                "Bindings 或 Access Catalog 不是有效 JSON",
              ),
            },
          };
        throw error;
      }
    },
  };
}

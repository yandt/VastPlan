import type { FormPageDefinition } from "@vastplan/workbench-sdk";
import { CollectionFormWorkflow } from "./CollectionFormWorkflow.js";

export function FormPage({ page }: { page: FormPageDefinition }) {
  return <CollectionFormWorkflow definition={page.form} selected={[]} open onRefresh={() => undefined} />;
}

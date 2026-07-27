import type { FormPageDefinition } from "@vastplan/workbench-sdk";
import { CollectionFormWorkflow } from "./CollectionFormWorkflow.js";
import { WorkbenchPageFlow } from "../layout/WorkbenchRhythm.js";

export function FormPage({ page }: { page: FormPageDefinition }) {
  return <WorkbenchPageFlow><CollectionFormWorkflow definition={page.form} selected={[]} open onRefresh={() => undefined} /></WorkbenchPageFlow>;
}

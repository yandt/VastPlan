import { usePortalI18n, usePortalUI, type PageRefreshSignal, type WorkbenchPreferencePort } from "@vastplan/ui-primitives";
import type { WorkbenchPresentationConfig, WorkspacePageDefinition } from "@vastplan/workbench-sdk";
import { CollectionPage } from "../collection/CollectionPage.js";
import { WorkbenchPageFlow } from "../layout/WorkbenchRhythm.js";

export function WorkspacePage({ page, preferenceScope, preferences, presentation, refreshSignal }: {
  page: WorkspacePageDefinition;
  preferenceScope: string;
  preferences?: WorkbenchPreferencePort;
  presentation?: WorkbenchPresentationConfig;
  refreshSignal?: PageRefreshSignal;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  return <WorkbenchPageFlow>
    {page.sections.map((section, index) => <ui.Panel key={section.id}>
      <ui.Stack gap="sm">
        <ui.Stack direction="row" gap="sm" align="center" wrap>
          <ui.Status tone="info">{index + 1}</ui.Status>
          <div>
            <strong>{i18n.text(section.page.title)}</strong>
            {section.page.description === undefined ? null : <div style={{ marginTop: 4, color: ui.theme.tokens.color.mutedText }}>{i18n.text(section.page.description)}</div>}
          </div>
        </ui.Stack>
        <CollectionPage page={section.page} preferenceScope={`${preferenceScope}/${page.id}/${section.id}`} preferences={preferences} presentation={presentation} refreshSignal={refreshSignal} />
      </ui.Stack>
    </ui.Panel>)}
  </WorkbenchPageFlow>;
}

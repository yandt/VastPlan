import type { FormPresentation, FormSchema, FormValidationResult, SizeableProps } from "@vastplan/ui-contract";

export interface FormRendererProps extends SizeableProps {
  schema: FormSchema;
  value: Record<string, unknown>;
  onChange(value: Record<string, unknown>): void;
  /** Component geometry only; form layout remains governed by presentation. */
  /** A framework-neutral field-label arrangement for composed Workbench forms. */
  presentation?: FormPresentation;
  presentationSection?: string;
  onPresentationSectionChange?(sectionID: string): void;
  readOnly?: boolean;
  submitting?: boolean;
  errors?: Readonly<Record<string, string>>;
  context?: Readonly<Record<string, unknown>>;
  validate?(request: { schema: FormSchema; value: Readonly<Record<string, unknown>>; context: Readonly<Record<string, unknown>>; signal: AbortSignal }): Promise<Readonly<Record<string, string>>>;
  validationDelayMs?: number;
  onValidationChange?(result: FormRendererValidationState): void;
}

export interface FormRendererValidationState extends FormValidationResult {
  errors: Readonly<Record<string, string>>;
  validating: boolean;
}

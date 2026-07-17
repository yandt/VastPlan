import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  Menu as ArcoMenu,
  Modal,
  Notification,
  Result,
  Spin,
  Typography,
} from "@arco-design/web-react";
import { createElement, useState } from "react";
import type { DesignSystemAdapter, FormField, FormRendererProps, MenuItem, PortalUI } from "@vastplan/portal-ui";
import { PortalUIProvider } from "@vastplan/portal-ui";

function Page({ title, actions, children }: { title?: string; actions?: React.ReactNode; children: React.ReactNode }) {
  return <main style={{ padding: 24 }}><header style={{ display: "flex", justifyContent: "space-between", gap: 16 }}><Typography.Title heading={4}>{title}</Typography.Title>{actions}</header>{children}</main>;
}

function Panel({ title, children }: { title?: string; children: React.ReactNode }) {
  return <Card title={title}>{children}</Card>;
}

function Menu({ items, activeID, onSelect }: { items: MenuItem[]; activeID?: string; onSelect?(id: string): void }) {
  return <ArcoMenu selectedKeys={activeID ? [activeID] : []} onClickMenuItem={(key) => onSelect?.(key)}>{items.map((item) => <ArcoMenu.Item key={item.id} disabled={item.disabled}>{item.label}</ArcoMenu.Item>)}</ArcoMenu>;
}

function visible(field: FormField, value: Record<string, unknown>): boolean {
  if (field.visibleWhen === undefined) return true;
  const current = value[field.visibleWhen.key];
  return field.visibleWhen.equals !== undefined ? current === field.visibleWhen.equals : current !== field.visibleWhen.notEquals;
}

/** The Arco adapter deliberately maps only semantic field types; no framework type leaks into a plugin schema. */
function FormRenderer({ schema, value, onChange, readOnly, submitting }: FormRendererProps) {
  const [local, setLocal] = useState(value);
  const update = (key: string, next: unknown) => {
    const changed = { ...local, [key]: next };
    setLocal(changed);
    onChange(changed);
  };
  return <Form layout="vertical">{schema.fields.filter((field) => visible(field, local)).map((field) => {
    const disabled = Boolean(readOnly || field.readOnly || field.disabled || submitting);
    if (field.type === "secretRef") {
      return <Form.Item key={field.key} label={field.title} extra={field.help}><Input value={String(local[field.key] ?? "")} placeholder="选择凭证引用" disabled={disabled} onChange={(next) => update(field.key, next)} /></Form.Item>;
    }
    if (field.type === "textarea") {
      return <Form.Item key={field.key} label={field.title} extra={field.help}><Input.TextArea value={String(local[field.key] ?? "")} disabled={disabled} onChange={(next) => update(field.key, next)} /></Form.Item>;
    }
    return <Form.Item key={field.key} label={field.title} extra={field.help}><Input value={String(local[field.key] ?? "")} disabled={disabled} onChange={(next) => update(field.key, next)} /></Form.Item>;
  })}</Form>;
}

const ui: PortalUI = {
  Page,
  Panel,
  Button: ({ children, ...props }) => <Button {...props}>{children}</Button>,
  Menu,
  FormRenderer,
  EmptyState: ({ title, description }) => <Result status="404" title={title} subTitle={description} />,
  ErrorState: ({ title, retry }) => <Alert type="error" title={title} action={retry ? <Button onClick={retry}>重试</Button> : undefined} />,
  Busy: ({ label }) => <Spin tip={label} />,
  notify: ({ title, content, kind = "info" }) => Notification[kind]({ title, content }),
  confirm: ({ title, content }) => new Promise((resolve) => Modal.confirm({ title, content, onOk: () => resolve(true), onCancel: () => resolve(false) })),
};

export const arcoDesignSystem: DesignSystemAdapter = {
  id: "ui.design-system",
  framework: "arco",
  uiContract: "1.0.0",
  capabilities: ["layout", "menu", "overlay", "form", "data", "feedback", "theme"],
  Provider: ({ children }) => createElement(PortalUIProvider, { ui }, children),
};

export default arcoDesignSystem;

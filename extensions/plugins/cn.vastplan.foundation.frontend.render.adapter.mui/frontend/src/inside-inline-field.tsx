import { Box, Tooltip } from "@mui/material";
import type { ReactNode } from "react";

export interface MuiFieldTemplateProps {
  id: string;
  label: string;
  children: ReactNode;
  description?: ReactNode;
  errors?: ReactNode;
  help?: ReactNode;
  hidden?: boolean;
  required?: boolean;
  displayLabel?: boolean;
  schema: { type?: unknown };
}

export function MuiInsideInlineFieldTemplate(props: MuiFieldTemplateProps) {
  if (props.hidden) return <Box sx={{ display: "none" }}>{props.children}</Box>;
  if (props.schema.type === "object" || props.schema.type === "array") return <>{props.children}{props.help}</>;
  const label = props.displayLabel === false ? "" : props.label;
  return <Box sx={{ mb: 0 }}><Box className="vp-mui-inside-inline-field" sx={{
    boxSizing: "border-box", width: "100%", minWidth: 0, minHeight: 32, display: "flex", alignItems: "center",
    border: 1, borderColor: "divider", borderRadius: 1, bgcolor: "background.paper",
    "&:focus-within": { borderColor: "primary.main", boxShadow: (theme) => `0 0 0 2px ${theme.palette.primary.main}1a` },
    "& .vp-inside-inline-label": { boxSizing: "border-box", flex: "0 1 auto", maxWidth: "40%", minWidth: 0, px: 1, color: "text.secondary", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", borderRight: 1, borderColor: "divider", cursor: "default" },
    "& .vp-inside-inline-control": { flex: 1, minWidth: 0 },
    "& .MuiOutlinedInput-notchedOutline": { border: "0!important" },
    "& .MuiInputBase-root": { width: "100%", boxShadow: "none", bgcolor: "transparent" },
    "& input, & select": { boxSizing: "border-box", width: "100%", minWidth: 0, border: 0, outline: 0, background: "transparent" },
    "@media (max-width:767px)": { "& .vp-inside-inline-label": { maxWidth: "45%" } },
  }}>
    {label === "" ? null : <Tooltip title={label}><Box component="label" htmlFor={props.id} className="vp-inside-inline-label" aria-label={label}>{label}{props.required ? <Box component="span" aria-hidden color="error.main"> *</Box> : null}</Box></Tooltip>}
    <Box className="vp-inside-inline-control">{props.children}</Box>
  </Box>{props.description}{props.errors}{props.help}</Box>;
}

export function MuiInlineFieldTemplate(props: MuiFieldTemplateProps) {
  if (props.hidden) return <Box sx={{ display: "none" }}>{props.children}</Box>;
  if (props.schema.type === "object" || props.schema.type === "array") return <>{props.children}{props.help}</>;
  const label = props.displayLabel === false ? "" : props.label;
  return <Box sx={{ mb: 2, display: "grid", gridTemplateColumns: { xs: "minmax(0,1fr)", sm: "112px minmax(0,1fr)" }, columnGap: 1.5, alignItems: "start" }}>
    {label === "" ? <Box /> : <Tooltip title={label}><Box component="label" htmlFor={props.id} sx={{ minWidth: 0, pt: 1, color: "text.secondary", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{label}{props.required ? <Box component="span" aria-hidden color="error.main"> *</Box> : null}</Box></Tooltip>}
    <Box sx={{ minWidth: 0 }}>{props.children}{props.description}{props.errors}{props.help}</Box>
  </Box>;
}

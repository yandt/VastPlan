declare module "*.css" {
  const content: string;
  export default content;
}

declare module "@arco-design/web-react/icon/react-icon/*" {
  import type { ForwardRefExoticComponent, RefAttributes, SVGAttributes } from "react";

  const Icon: ForwardRefExoticComponent<SVGAttributes<SVGElement> & {
    spin?: boolean;
  } & RefAttributes<unknown>>;
  export default Icon;
}

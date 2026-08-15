import { tool } from "@opencode-ai/plugin";
import { callMarvo } from "../marvo-tool";

export default tool({
  description: "读取 Marvo 空间信息，或按用户明确要求修改空间品牌名称。",
  args: {
    action: tool.schema.enum(["get", "set_brand"]),
    name: tool.schema
      .string()
      .max(100)
      .optional()
      .describe("set_brand 所需的新品牌名称"),
  },
  async execute(args) {
    return callMarvo("space", args);
  },
});

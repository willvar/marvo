import { tool } from "@opencode-ai/plugin";
import { callMarvo } from "../marvo-tool";

export default tool({
  description:
    "管理用户的 Marvo 记忆。只有用户明确表达长期偏好或纠正既有偏好时才新增或修改；把负面反馈转成正向、可执行的单条记忆。不要记录当前任务要求、事实、权限规则或敏感信息。",
  args: {
    action: tool.schema.enum(["list", "add", "update", "remove"]),
    id: tool.schema
      .string()
      .optional()
      .describe("update 或 remove 所需的记忆 ID"),
    text: tool.schema
      .string()
      .max(4096)
      .optional()
      .describe("add 或 update 所需的单行记忆内容"),
  },
  async execute(args) {
    return callMarvo("memories", args);
  },
});

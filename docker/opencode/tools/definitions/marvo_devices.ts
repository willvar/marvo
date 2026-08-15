import { tool } from "@opencode-ai/plugin";
import { callMarvo } from "../marvo-tool";

export default tool({
  description:
    "按用户明确要求管理当前 Marvo 空间的设备申请与已批准设备。批准、拒绝或撤销前，应确认目标设备信息，避免误操作。",
  args: {
    action: tool.schema.enum(["list", "approve", "reject", "rename", "revoke"]),
    id: tool.schema
      .string()
      .optional()
      .describe("approve 或 reject 所需的申请 ID"),
    local_device_id: tool.schema
      .string()
      .optional()
      .describe("rename 或 revoke 所需的本地设备 ID"),
    name: tool.schema
      .string()
      .max(50)
      .optional()
      .describe("rename 所需的新名称"),
  },
  async execute(args) {
    return callMarvo("devices", args);
  },
});

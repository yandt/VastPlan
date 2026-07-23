import fs from "node:fs";
import path from "node:path";

const maximumStateBytes = 8 << 20;

export class ProfileStateStore {
  constructor(stateFile) {
    if (typeof stateFile !== "string" || !path.isAbsolute(stateFile) || path.normalize(stateFile) !== stateFile) throw new Error("Webhook Profile stateFile 无效");
    this.stateFile = stateFile;
    this.#ensureDirectory();
  }

  load() {
    let stat;
    try { stat = fs.lstatSync(this.stateFile); }
    catch (error) {
      if (error.code === "ENOENT") return undefined;
      throw error;
    }
    if (!stat.isFile() || stat.isSymbolicLink() || (stat.mode & 0o077) !== 0 || stat.size < 2 || stat.size > maximumStateBytes) throw new Error("Webhook Profile 状态文件必须是仅属主可访问且大小受限的普通文件");
    return JSON.parse(fs.readFileSync(this.stateFile, "utf8"));
  }

  save(state) {
    const raw = Buffer.from(JSON.stringify(state));
    if (raw.length < 2 || raw.length > maximumStateBytes) throw new Error("Webhook Profile 状态超过大小上限");
    const directory = path.dirname(this.stateFile);
    const temporary = path.join(directory, `.webhook-profiles-${process.pid}-${Date.now()}-${Math.random().toString(16).slice(2)}`);
    let descriptor;
    try {
      descriptor = fs.openSync(temporary, "wx", 0o600);
      fs.writeFileSync(descriptor, raw);
      fs.fsyncSync(descriptor);
      fs.closeSync(descriptor);
      descriptor = undefined;
      fs.renameSync(temporary, this.stateFile);
      const directoryDescriptor = fs.openSync(directory, "r");
      try { fs.fsyncSync(directoryDescriptor); } finally { fs.closeSync(directoryDescriptor); }
    } finally {
      if (descriptor !== undefined) fs.closeSync(descriptor);
      try { fs.unlinkSync(temporary); } catch (error) { if (error.code !== "ENOENT") throw error; }
      raw.fill(0);
    }
  }

  #ensureDirectory() {
    const directory = path.dirname(this.stateFile);
    fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
    const stat = fs.lstatSync(directory);
    if (!stat.isDirectory() || stat.isSymbolicLink() || (stat.mode & 0o022) !== 0) throw new Error("Webhook Profile 状态目录不可由 group/other 写入且不能是符号链接");
  }
}

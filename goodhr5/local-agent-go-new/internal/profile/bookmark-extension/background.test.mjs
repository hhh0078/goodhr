// 文件作用说明：回归验证书签扩展补齐默认入口时保留已有书签、文件夹及重复启动的幂等性。
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import vm from "node:vm";
import test from "node:test";

/** createBookmarkAPI 创建具备排序语义的本地书签接口替身。 */
function createBookmarkAPI() {
  const nodes = new Map();
  let nextID = 1;
  /** children 按位置读取指定文件夹中的节点。 */
  const children = (parentId) => [...nodes.values()].filter((node) => node.parentId === parentId).sort((a, b) => a.index - b.index);
  /** insert 把节点放入指定位置，并重新计算同级位置。 */
  const insert = (node, parentId, index) => {
    const siblings = children(parentId).filter((item) => item.id !== node.id);
    siblings.splice(index ?? siblings.length, 0, node);
    siblings.forEach((item, position) => { item.parentId = parentId; item.index = position; });
    nodes.set(node.id, node);
    return { ...node };
  };
  return {
    nodes,
    /** getChildren 返回指定文件夹节点副本。 */
    async getChildren(parentId) { return children(parentId).map((node) => ({ ...node })); },
    /** create 创建节点并更新同级排序。 */
    async create(details) { return insert({ ...details, id: String(nextID++) }, details.parentId, details.index); },
    /** move 保留节点内容并更新所在位置。 */
    async move(id, destination) { return insert(nodes.get(id), destination.parentId, destination.index); },
    /** remove 只移除指定节点并更新同级位置，供退役默认书签的回归验证。 */
    async remove(id) {
      const node = nodes.get(id);
      assert.ok(node);
      nodes.delete(id);
      children(node.parentId).forEach((item, index) => { item.index = index; });
    },
  };
}

/** 验证默认书签置前、旧书签层级迁移、用户节点保留及第二次执行不新增重复节点。 */
test("迁移与默认入口补齐保留已有书签，重复启动不重复添加", async () => {
  const context = vm.createContext({});
  vm.runInContext(await fs.readFile(new URL("./background.js", import.meta.url), "utf8"), context);
  const bookmarks = createBookmarkAPI();
  const native = await bookmarks.create({ parentId: "toolbar_____", title: "原有 Firefox 收藏", url: "https://native.example/" });
  const existingDefault = await bookmarks.create({ parentId: "toolbar_____", title: "我的自定义名字", url: "https://default.example/" });
  const configuration = {
    defaults: [{ title: "默认入口", url: "https://default.example/" }, { title: "另一个入口", url: "https://second.example/" }],
    toolbar: [{ title: "旧文件夹", children: [{ title: "旧收藏", url: "https://legacy.example/" }] }],
    other: [{ title: "其他收藏", url: "https://other.example/" }],
  };
  await context.initializeBookmarks(bookmarks, configuration);
  const firstCount = bookmarks.nodes.size;
  await context.initializeBookmarks(bookmarks, configuration);
  assert.equal(bookmarks.nodes.size, firstCount);
  const toolbar = await bookmarks.getChildren("toolbar_____");
  assert.equal(toolbar[0].id, existingDefault.id);
  assert.equal(toolbar[0].title, "我的自定义名字");
  assert.equal(toolbar[2].id, native.id);
  const folder = toolbar.find((node) => node.title === "旧文件夹");
  assert.equal((await bookmarks.getChildren(folder.id))[0].url, "https://legacy.example/");
  assert.equal((await bookmarks.getChildren("unfiled_____"))[0].url, "https://other.example/");
});

/** 验证旧 goodhr5 默认入口不会迁移复活，且用户改名、同名其他地址、文件夹及前程自定义入口均保留。 */
test("退役旧默认入口并补齐前程无忧，保留用户书签且重复启动幂等", async () => {
  const context = vm.createContext({});
  vm.runInContext(await fs.readFile(new URL("./background.js", import.meta.url), "utf8"), context);
  const bookmarks = createBookmarkAPI();
  const retired = await bookmarks.create({ parentId: "toolbar_____", title: "goodhr5.58it.cn", url: "https://goodhr5.58it.cn/" });
  const retiredNoSlash = await bookmarks.create({ parentId: "toolbar_____", title: "GOODHR5.58IT.CN", url: "https://goodhr5.58it.cn" });
  const renamed = await bookmarks.create({ parentId: "toolbar_____", title: "我的旧站收藏", url: "https://goodhr5.58it.cn/" });
  const sameTitle = await bookmarks.create({ parentId: "toolbar_____", title: "goodhr5.58it.cn", url: "https://example.com/" });
  const subpath = await bookmarks.create({ parentId: "toolbar_____", title: "goodhr5.58it.cn", url: "https://goodhr5.58it.cn/notes" });
  const query = await bookmarks.create({ parentId: "toolbar_____", title: "goodhr5.58it.cn", url: "https://goodhr5.58it.cn/?from=saved" });
  const folder = await bookmarks.create({ parentId: "toolbar_____", title: "goodhr5.58it.cn" });
  const nested = await bookmarks.create({ parentId: folder.id, title: "goodhr5.58it.cn", url: "https://goodhr5.58it.cn/" });
  const other = await bookmarks.create({ parentId: "unfiled_____", title: "goodhr5.58it.cn", url: "https://goodhr5.58it.cn/" });
  const job51 = await bookmarks.create({ parentId: "toolbar_____", title: "我的前程入口", url: "https://mall.51job.com/" });
  const configuration = {
    defaults: [{ title: "GoodHR", url: "https://goodhr.58it.cn/" }, { title: "前程无忧", url: "https://mall.51job.com/" }],
    toolbar: [{ title: "goodhr5.58it.cn", url: "https://goodhr5.58it.cn/" }],
  };
  await context.initializeBookmarks(bookmarks, configuration);
  const count = bookmarks.nodes.size;
  await context.initializeBookmarks(bookmarks, configuration);
  assert.equal(bookmarks.nodes.size, count);
  assert.equal(bookmarks.nodes.has(retired.id), false);
  assert.equal(bookmarks.nodes.has(retiredNoSlash.id), false);
  for (const node of [renamed, sameTitle, subpath, query, folder, nested, other]) assert.ok(bookmarks.nodes.has(node.id));
  const toolbar = await bookmarks.getChildren("toolbar_____");
  assert.equal(toolbar[1].id, job51.id);
  assert.equal(toolbar[1].title, "我的前程入口");
  assert.equal(toolbar.filter((node) => node.url === "https://mall.51job.com/").length, 1);
  assert.equal(toolbar.filter((node) => node.title.toLowerCase() === "goodhr5.58it.cn" && node.url === "https://goodhr5.58it.cn/").length, 0);
});

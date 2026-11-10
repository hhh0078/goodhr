// 文件作用说明：通过 Firefox 自身书签接口补齐默认入口与旧书签，不访问或注入网页。

/** mergeBookmarks 保留已有节点，按父文件夹补齐书签，并维持默认入口顺序。 */
async function mergeBookmarks(bookmarks, parentId, nodes, pinToFront = false) {
  let index = 0;
  for (const node of nodes) {
    const children = await bookmarks.getChildren(parentId);
    let existing = children.find((child) => node.url
      ? child.url === node.url
      : !child.url && child.title === node.title);
    if (!existing) {
      existing = await bookmarks.create({ parentId, title: node.title,
        ...(node.url ? { url: node.url } : {}),
        ...(pinToFront ? { index } : {}),
      });
    } else if (pinToFront && existing.index !== index) {
      existing = await bookmarks.move(existing.id, { parentId, index });
    }
    if (!node.url) await mergeBookmarks(bookmarks, existing.id, node.children ?? []);
    index++;
  }
}

/** isRetiredDefaultBookmark 仅识别旧程序默认的 goodhr5 根入口，保留改名、子路径及同名的其他地址。 */
function isRetiredDefaultBookmark(node) {
  return typeof node.title === "string" && node.title.trim().toLowerCase() === "goodhr5.58it.cn"
    && (node.url === "https://goodhr5.58it.cn/" || node.url === "https://goodhr5.58it.cn");
}

/** initializeBookmarks 只移除书签栏中明确退役的默认入口，补齐默认书签并保留其他分组。 */
async function initializeBookmarks(bookmarks, configuration) {
  for (const node of await bookmarks.getChildren("toolbar_____")) {
    if (isRetiredDefaultBookmark(node)) await bookmarks.remove(node.id);
  }
  await mergeBookmarks(bookmarks, "toolbar_____", configuration.defaults, true);
  await mergeBookmarks(bookmarks, "toolbar_____", (configuration.toolbar ?? []).filter((node) => !isRetiredDefaultBookmark(node)));
  await mergeBookmarks(bookmarks, "unfiled_____", configuration.other ?? []);
  await mergeBookmarks(bookmarks, "mobile______", configuration.mobile ?? []);
}

// 扩展没有网页权限或内容脚本；补齐操作可重复执行，不产生重复书签。
if (typeof browser !== "undefined") {
  fetch(browser.runtime.getURL("bookmarks.json"))
    .then((response) => response.json())
    .then((configuration) => initializeBookmarks(browser.bookmarks, configuration))
    .catch((error) => console.error("GoodHR 书签没有补齐，请重新打开浏览器", error));
}

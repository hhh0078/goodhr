<template>
  <section class="panel">
    <div class="panel-header"><h2>任务列表</h2><button class="ghost" @click="tasks.load">刷新</button></div>

    <!-- 创建任务折叠 -->
    <div class="panel-header" style="margin-bottom:8px;border:none;padding-bottom:0">
      <button class="ghost" @click="showCreate=!showCreate">{{ showCreate ? '收起创建' : '+ 创建任务' }}</button>
    </div>
    <div v-if="showCreate" class="form-grid" style="margin-bottom:12px">
      <label>平台<select v-model="tasks.form.value.platformId">
        <option value="boss">Boss直聘</option><option value="zhaopin">智联招聘</option><option value="liepin">猎聘</option>
      </select></label>
      <label>账号<select v-model="tasks.form.value.platformAccountId">
        <option value="">请选择账号</option>
        <option v-for="acc in accounts" :key="acc.id" :value="acc.id">{{ acc.display_name }}</option>
      </select></label>
      <label>岗位模板<select v-model="tasks.form.value.positionId">
        <option value="">不使用模板</option>
        <option v-for="pos in positions" :key="pos.id" :value="pos.id">{{ pos.name }}</option>
      </select></label>
      <label>筛选模式<select v-model="tasks.form.value.mode">
        <option value="keyword">关键词筛选</option><option value="ai">AI筛选</option>
      </select></label>
      <label>匹配上限<input v-model="tasks.form.value.matchLimit" type="number" min="1" /></label>
    </div>
    <div v-if="showCreate" class="actions">
      <button :disabled="tasks.loading.value||!tasks.form.value.platformAccountId" @click="createTask">{{ tasks.loading.value?'创建中...':'创建任务' }}</button>
    </div>

    <p v-if="tasks.tasks.value.length === 0" class="hint">暂无任务</p>
    <p v-if="tasks.message.value" class="success">{{ tasks.message.value }}</p>
    <p v-if="tasks.error.value" class="error">{{ tasks.error.value }}</p>

    <div v-else class="card-list">
      <article v-for="task in tasks.tasks.value" :key="task.id" class="card task-card">
        <div class="task-main">
          <div class="task-title">
            <strong>{{ task.platform_account_name || task.platform_account_id }}</strong>
            <p class="card-meta">{{ task.platform_id }} / {{ task.mode }} / 上限 {{ task.match_limit }}</p>
            <p v-if="task.position_name" class="card-meta">岗位模板：{{ task.position_name }}</p>
          </div>
          <div class="task-stats">
            <span class="stat-chip">状态 {{ task.status }}</span>
            <span class="stat-chip">扫描 {{ task.scanned_count }}</span>
            <span class="stat-chip">打招呼 {{ task.greeted_count }}</span>
            <span class="stat-chip">跳过 {{ task.skipped_count }}</span>
            <span class="stat-chip">失败 {{ task.failed_count }}</span>
          </div>
        </div>

        <div class="actions compact task-actions">
          <button class="ghost primary" :disabled="tasks.loading.value" @click="tasks.execute(task.id)">运行</button>
          <button class="ghost danger" :disabled="tasks.loading.value || task.status !== 'running'" @click="tasks.stop(task.id)">停止</button>
          <button class="ghost" @click="tasks.toggleLogs(task.id)">
            {{ tasks.expandedTaskId.value === task.id ? '收起日志' : '展开日志' }}
          </button>
          <button class="ghost" @click="tasks.toggleCandidates(task)">
            {{ tasks.candidateExpandedTaskId.value === tasks.localTaskID(task) ? '收起候选人' : '查看候选人' }}
          </button>
        </div>

        <!-- 日志面板 -->
        <div v-if="tasks.expandedTaskId.value === task.id" class="log-panel">
          <p v-if="!tasks.taskLogs.value[task.id] || tasks.taskLogs.value[task.id].length === 0" class="hint">暂无日志</p>
          <ol v-else>
            <li v-for="log in tasks.taskLogs.value[task.id]" :key="log.id">
              <span :class="{ error: log.level === 'error', warn: log.level === 'warn' }">{{ log.level }}</span>
              <strong>{{ log.message }}</strong>
            </li>
          </ol>
        </div>

        <!-- 候选人面板 -->
        <div v-if="tasks.candidateExpandedTaskId.value === tasks.localTaskID(task)" class="log-panel">
          <button class="ghost" :disabled="tasks.candidateLoadingTaskId.value === tasks.localTaskID(task)" @click="tasks.loadCandidates(task, tasks.localTaskID(task))" style="margin-bottom:8px">
            {{ tasks.candidateLoadingTaskId.value === tasks.localTaskID(task) ? '读取中...' : '刷新候选人' }}
          </button>

          <p v-if="tasks.candidateError.value" class="error">{{ tasks.candidateError.value }}</p>

          <div v-if="tasks.taskPositionSnapshot(task).name" class="snapshot">
            <strong>{{ tasks.taskPositionSnapshot(task).name }}</strong>
            <p class="snapshot-meta">关键词：{{ (tasks.taskPositionSnapshot(task).keywords || []).join(' / ') || '无' }}</p>
            <p class="snapshot-meta">排除词：{{ (tasks.taskPositionSnapshot(task).exclude_keywords || []).join(' / ') || '无' }}</p>
          </div>

          <p v-if="tasks.candidateItems(task).length === 0" class="hint">暂无候选人数据</p>

          <div v-else class="card-list" style="margin-top:8px">
            <article v-for="c in tasks.candidateItems(task)" :key="c.id" class="card">
              <div>
                <strong>{{ tasks.candidateTitle(c) }}</strong>
                <p class="card-meta">{{ tasks.candidateSubtitle(c) }}</p>
                <p v-if="tasks.candidateDetail(c)" class="candidate-detail">{{ tasks.candidateDetail(c) }}</p>
              </div>
              <button class="ghost danger" @click="tasks.removeCandidate(task, c)">删除</button>
            </article>
          </div>
        </div>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { listPlatformAccounts } from '../services/cloudApi'
const props = defineProps({ tasks: Object, positions: Object, token: String, agent: Object })
const showCreate = ref(false)
const accounts = ref<any[]>([])
const accountsError = ref('')
async function loadAccounts() { accountsError.value=''; try{ accounts.value=await listPlatformAccounts() }catch(e:any){accountsError.value=e.message} }
async function createTask() { if(props.tasks) await props.tasks.create(); showCreate.value=false; await loadAccounts() }
onMounted(loadAccounts)
</script>

<style scoped>
.task-card {
  display: block;
}
.task-main {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}
.task-title {
  min-width: 0;
}
.task-stats {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  justify-content: flex-end;
}
.stat-chip {
  border: 1px solid #333;
  color: #ddd;
  padding: 2px 8px;
  font-size: 12px;
  line-height: 1.3;
}
.task-actions {
  margin-top: 8px;
}
@media (max-width: 900px) {
  .task-main {
    flex-direction: column;
    align-items: flex-start;
  }
  .task-stats {
    justify-content: flex-start;
  }
}
</style>

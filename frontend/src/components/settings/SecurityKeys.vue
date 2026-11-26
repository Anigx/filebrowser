<template>
  <section class="card">
    <div class="card-title">
      <h2>{{ t("securityKeys.title") }}</h2>
    </div>
    <div class="card-content">
      <p v-if="error" role="alert">{{ error }}</p>
      <p v-if="loading" role="status">{{ t("securityKeys.loading") }}</p>
      <template v-else-if="loaded">
        <p v-if="keys.length === 0">{{ t("securityKeys.empty") }}</p>
        <p v-else>{{ t("securityKeys.active") }}</p>
        <ul v-if="keys.length">
          <li v-for="key in keys" :key="key.id">
            {{ key.label }}
            <button
              class="button button--flat button--red"
              type="button"
              :disabled="
                pending ||
                !canConfirm ||
                (isSelf && lockPassword && keys.length === 1)
              "
              @click="select(keys.length === 1 ? 'reset' : key.id)"
            >
              {{ t("securityKeys.remove") }}
            </button>
          </li>
        </ul>
        <p v-if="!canConfirm || (isSelf && !canUseKey)">
          {{ t("securityKeys.unsupported") }}
        </p>
        <form
          v-if="
            isSelf &&
            canUseKey &&
            (keys.length || !lockPassword) &&
            keys.length < 5
          "
          @submit.prevent="add"
        >
          <p>
            {{
              t(
                keys.length
                  ? "securityKeys.backup"
                  : "securityKeys.replacesPassword"
              )
            }}
          </p>
          <label>
            {{ t("securityKeys.label") }}
            <input
              v-model.trim="label"
              class="input input--block"
              maxlength="80"
              required
              :disabled="pending"
            />
          </label>
          <label v-if="!keys.length">
            {{ t("settings.currentPassword") }}
            <input
              v-model="enrollmentPassword"
              class="input input--block"
              type="password"
              autocomplete="current-password"
              required
              :disabled="pending"
            />
          </label>
          <button class="button button--flat" type="submit" :disabled="pending">
            {{ t("securityKeys.add") }}
          </button>
        </form>

        <button
          v-if="keys.length && (!isSelf || !lockPassword) && !selection"
          class="button button--flat button--red"
          type="button"
          :disabled="pending || !canConfirm"
          @click="select('reset')"
        >
          {{ t(isSelf ? "securityKeys.switchBack" : "securityKeys.reset") }}
        </button>
        <form v-if="selection" @submit.prevent="remove">
          <p>
            {{
              t(
                selection === "reset"
                  ? "securityKeys.requireNewPassword"
                  : "securityKeys.confirmRemove"
              )
            }}
          </p>
          <label v-if="!authStore.user?.keyOnly">
            {{ t("settings.currentPassword") }}
            <input
              v-model="currentPassword"
              class="input input--block"
              type="password"
              autocomplete="current-password"
              required
              :disabled="pending"
            />
          </label>
          <template v-if="selection === 'reset'">
            <label>
              {{ t("settings.newPassword") }}
              <input
                v-model="newPassword"
                class="input input--block"
                type="password"
                autocomplete="new-password"
                required
                :disabled="pending"
              />
            </label>
            <label>
              {{ t("settings.newPasswordConfirm") }}
              <input
                v-model="confirmPassword"
                class="input input--block"
                type="password"
                autocomplete="new-password"
                required
                :disabled="pending"
              />
            </label>
          </template>
          <button
            class="button button--flat button--red"
            type="submit"
            :disabled="pending"
          >
            {{
              t(
                selection === "reset"
                  ? "securityKeys.confirmReset"
                  : "securityKeys.remove"
              )
            }}
          </button>
          <button
            class="button button--flat"
            type="button"
            :disabled="pending"
            @click="select('')"
          >
            {{ t("buttons.cancel") }}
          </button>
        </form>
      </template>
      <p v-if="pending" role="status">{{ t("securityKeys.waiting") }}</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import * as api from "@/api/securityKeys";
import type { SecurityKey } from "@/api/securityKeys";
import { useAuthStore } from "@/stores/auth";
import { logout } from "@/utils/auth";
import {
  isSecurityKeyCancellation,
  supportsSecurityKeys,
} from "@/utils/webauthn";

const props = defineProps<{
  userId: number;
  isSelf: boolean;
  lockPassword: boolean;
}>();
const emit = defineEmits<{ active: [boolean] }>();
const { t } = useI18n();
const authStore = useAuthStore();
const canUseKey = supportsSecurityKeys();
const canConfirm = computed(() => !authStore.user?.keyOnly || canUseKey);
const keys = ref<SecurityKey[]>([]);
const pending = ref(false);
const loading = ref(true);
const loaded = ref(false);
const error = ref("");
const label = ref("");
const enrollmentPassword = ref("");
const selection = ref("");
const currentPassword = ref("");
const newPassword = ref("");
const confirmPassword = ref("");

function showError(err: unknown) {
  error.value = t(
    isSecurityKeyCancellation(err)
      ? "securityKeys.canceled"
      : "securityKeys.failed"
  );
}

async function refresh() {
  keys.value = (await api.list(props.userId)).keys;
  emit("active", keys.value.length > 0);
  loaded.value = true;
}
onMounted(async () => {
  try {
    await refresh();
  } catch (err) {
    showError(err);
  } finally {
    loading.value = false;
  }
});

function select(value: string) {
  selection.value = value;
  currentPassword.value = newPassword.value = confirmPassword.value = "";
  error.value = "";
}

async function add() {
  pending.value = true;
  error.value = "";
  try {
    const proof = keys.value.length
      ? await api.verify("add-key", props.userId)
      : undefined;
    await api.enroll(
      label.value,
      proof ? undefined : enrollmentPassword.value,
      proof
    );
    await logout();
  } catch (err) {
    showError(err);
  } finally {
    pending.value = false;
    enrollmentPassword.value = "";
  }
}

async function remove() {
  error.value = "";
  const reset = selection.value === "reset";
  if (reset && newPassword.value !== confirmPassword.value) {
    error.value = t("login.passwordsDontMatch");
    return;
  }
  pending.value = true;
  try {
    const keyID = reset ? undefined : selection.value;
    const proof = authStore.user?.keyOnly
      ? await api.verify(
          reset ? "reset-keys" : "remove-key",
          props.userId,
          keyID
        )
      : undefined;
    await api.remove(props.userId, {
      keyID,
      proof,
      currentPassword: proof ? undefined : currentPassword.value,
      newPassword: reset ? newPassword.value : undefined,
    });
    select("");
    if (props.isSelf) await logout();
    else await refresh();
  } catch (err) {
    showError(err);
    currentPassword.value = newPassword.value = confirmPassword.value = "";
  } finally {
    pending.value = false;
  }
}
</script>

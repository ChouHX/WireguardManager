import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ActionIcon,
  Alert,
  Avatar,
  Badge,
  Box,
  Button,
  Card,
  Divider,
  Group,
  Menu,
  Modal,
  PasswordInput,
  Select,
  Stack,
  Table,
  Text,
  TextInput,
} from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import {
  IconAlertTriangle,
  IconDotsVertical,
  IconPencil,
  IconSearch,
  IconTrash,
  IconUsers,
} from '@tabler/icons-react';

import { EmptyState, ErrorAlert } from '@/components/common/Feedback';
import { InlineLoader } from '@/components/common/LoadingScreen';
import { PageHeader } from '@/components/common/PageHeader';
import { useTranslation } from '@/i18n';
import { formatDateTime, messageOf } from '@/lib/format';
import { adminService } from '@/services';
import { useAuthStore } from '@/stores/auth-store';
import type { UpdateUserRequest, User, UserRole } from '@/types/auth';

interface EditFormValues {
  name: string;
  email: string;
  password: string;
  role: UserRole;
}

const EMPTY_FORM: EditFormValues = {
  name: '',
  email: '',
  password: '',
  role: 'normal_user',
};

const initialOf = (user: User) => (user.name || user.email).trim().charAt(0).toUpperCase();

export default function UsersPage() {
  const { t, locale } = useTranslation();
  const currentUser = useAuthStore((state) => state.user);

  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [search, setSearch] = useState('');
  const [selected, setSelected] = useState<User | null>(null);

  const [editOpened, editModal] = useDisclosure(false);
  const [deleteOpened, deleteModal] = useDisclosure(false);

  const localeTag = locale === 'zh' ? 'zh-CN' : 'en-US';

  const form = useForm<EditFormValues>({
    initialValues: EMPTY_FORM,
    validate: {
      name: (value) => (value.trim().length > 0 ? null : t('validation.required')),
      email: (value) =>
        /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value.trim()) ? null : t('validation.email'),
    },
  });

  const loadUsers = useCallback(async () => {
    const response = await adminService.getUsers();
    if (response.success && response.data) {
      setUsers(response.data);
    }
  }, []);

  const bootstrap = useCallback(async () => {
    try {
      setError(null);
      await loadUsers();
    } catch (err) {
      setError(messageOf(err, t('errors.somethingWrong')));
    } finally {
      setLoading(false);
    }
  }, [loadUsers, t]);

  useEffect(() => {
    void bootstrap();
  }, [bootstrap]);

  const roleOptions = useMemo(
    () => [
      { value: 'admin', label: t('user.roleAdmin') },
      { value: 'normal_user', label: t('user.roleUser') },
    ],
    [t],
  );

  const crumbs = useMemo(
    () => [{ label: t('breadcrumb.home'), to: '/dashboard' }, { label: t('breadcrumb.users') }],
    [t],
  );

  const filtered = useMemo(() => {
    const keyword = search.trim().toLowerCase();
    if (!keyword) return users;
    return users.filter(
      (item) =>
        item.name.toLowerCase().includes(keyword) || item.email.toLowerCase().includes(keyword),
    );
  }, [users, search]);

  /** 不可删除的原因：自己 / 管理员身份 */
  const deleteBlockReason = useCallback(
    (user: User) => {
      if (currentUser && user.id === currentUser.id) return t('users.cannotDeleteSelf');
      if (user.role === 'admin') return t('users.cannotDeleteAdmin');
      return null;
    },
    [currentUser, t],
  );

  const openEdit = (user: User) => {
    setSelected(user);
    form.setValues({ name: user.name, email: user.email, password: '', role: user.role });
    form.resetDirty({ name: user.name, email: user.email, password: '', role: user.role });
    editModal.open();
  };

  const openDelete = (user: User) => {
    setSelected(user);
    deleteModal.open();
  };

  const handleEdit = async () => {
    if (!selected) return;
    if (form.validate().hasErrors) return;

    const values = form.getValues();
    const payload: UpdateUserRequest = {};
    if (values.name !== selected.name) payload.name = values.name;
    if (values.email !== selected.email) payload.email = values.email;
    if (values.role !== selected.role) payload.role = values.role;
    if (values.password.trim().length > 0) payload.password = values.password;

    if (Object.keys(payload).length === 0) {
      notifications.show({ color: 'yellow', message: t('user.noFieldsChanged') });
      return;
    }

    setSubmitting(true);
    setError(null);
    try {
      const response = await adminService.updateUser(selected.id, payload);
      if (response.success) {
        notifications.show({ color: 'teal', message: t('users.updateSuccess') });
        editModal.close();
        setSelected(null);
        form.setValues(EMPTY_FORM);
        await loadUsers();
      } else {
        setError(response.message || t('users.updateFailed'));
      }
    } catch (err) {
      setError(messageOf(err, t('users.updateFailed')));
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async () => {
    if (!selected) return;

    setSubmitting(true);
    setError(null);
    try {
      const response = await adminService.deleteUser(selected.id);
      if (response.success) {
        notifications.show({ color: 'teal', message: t('users.deleteSuccess') });
        deleteModal.close();
        setSelected(null);
        await loadUsers();
      } else {
        setError(response.message || t('users.deleteFailed'));
      }
    } catch (err) {
      setError(messageOf(err, t('users.deleteFailed')));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Stack gap="md">
      <PageHeader title={t('users.title')} subtitle={t('users.allUsers')} crumbs={crumbs} />

      <ErrorAlert message={error} onClose={() => setError(null)} />

      <Card className="wm-rise" style={{ '--wm-delay': '120ms' } as React.CSSProperties}>
        <Group justify="space-between" align="center" mb="sm" gap="md">
          <Box>
            <Text fw={650}>{t('users.userList')}</Text>
            <Text size="xs" c="dimmed" mt={3}>
              {t('users.allUsers')}
            </Text>
          </Box>
          <Group gap="sm" wrap="nowrap">
            <Badge variant="light" color="gray" size="sm">
              {users.length}
            </Badge>
            <TextInput
              leftSection={<IconSearch size={15} />}
              placeholder={t('common.search')}
              value={search}
              onChange={(event) => setSearch(event.currentTarget.value)}
              radius="xs"
              w={{ base: 180, sm: 280 }}
            />
          </Group>
        </Group>
        <Divider mb="sm" variant="dashed" />

        {loading ? (
          <InlineLoader label={t('common.loading')} />
        ) : filtered.length === 0 ? (
          <EmptyState
            title={t('users.noUsers')}
            description={t('users.allUsers')}
            icon={<IconUsers size={22} />}
          />
        ) : (
          <div className="wm-table-scroll">
            <Table striped highlightOnHover verticalSpacing="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>{t('users.user')}</Table.Th>
                  <Table.Th>{t('common.role')}</Table.Th>
                  <Table.Th>{t('user.userId')}</Table.Th>
                  <Table.Th>{t('common.createdAt')}</Table.Th>
                  <Table.Th w={70} ta="right">
                    {t('common.actions')}
                  </Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {filtered.map((user) => {
                  const blockReason = deleteBlockReason(user);
                  return (
                    <Table.Tr key={user.id}>
                      <Table.Td>
                        <Group gap="sm" wrap="nowrap">
                          <Avatar color="wg" variant="light" radius="50%" size={34}>
                            {initialOf(user)}
                          </Avatar>
                          <Stack gap={2}>
                            <Text size="sm" fw={600}>
                              {user.name}
                            </Text>
                            <Text size="xs" c="dimmed">
                              {user.email}
                            </Text>
                          </Stack>
                        </Group>
                      </Table.Td>
                      <Table.Td>
                        {user.role === 'admin' ? (
                          <Badge color="wg">{t('user.roleAdmin')}</Badge>
                        ) : (
                          <Badge color="gray" variant="light">
                            {t('user.roleUser')}
                          </Badge>
                        )}
                      </Table.Td>
                      <Table.Td>
                        <Text size="xs" c="dimmed" className="wm-mono">
                          {user.user_uid}
                        </Text>
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm" className="wm-mono">
                          {formatDateTime(user.created_at, localeTag)}
                        </Text>
                      </Table.Td>
                      <Table.Td ta="right">
                        <Menu shadow="md" width={210} position="bottom-end" radius="xs" withinPortal>
                          <Menu.Target>
                            <ActionIcon
                              variant="subtle"
                              color="gray"
                              aria-label={t('common.actions')}
                            >
                              <IconDotsVertical size={17} />
                            </ActionIcon>
                          </Menu.Target>
                          <Menu.Dropdown>
                            <Menu.Item
                              leftSection={<IconPencil size={15} />}
                              onClick={() => openEdit(user)}
                            >
                              {t('common.edit')}
                            </Menu.Item>
                            <Menu.Divider />
                            <Menu.Item
                              color="red"
                              leftSection={<IconTrash size={15} />}
                              disabled={Boolean(blockReason)}
                              onClick={() => openDelete(user)}
                            >
                              {t('users.deleteUser')}
                            </Menu.Item>
                            {blockReason ? <Menu.Label>{blockReason}</Menu.Label> : null}
                          </Menu.Dropdown>
                        </Menu>
                      </Table.Td>
                    </Table.Tr>
                  );
                })}
              </Table.Tbody>
            </Table>
          </div>
        )}
      </Card>

      {/* 编辑用户 */}
      <Modal
        opened={editOpened}
        onClose={editModal.close}
        title={t('users.editUser')}
        radius="sm"
      >
        <Stack gap="md">
          <TextInput
            label={t('common.name')}
            placeholder={t('auth.namePlaceholder')}
            disabled={submitting}
            {...form.getInputProps('name')}
          />
          <TextInput
            label={t('common.email')}
            placeholder={t('auth.emailPlaceholder')}
            disabled={submitting}
            {...form.getInputProps('email')}
          />
          <PasswordInput
            label={t('user.newPassword')}
            placeholder={t('user.newPasswordPlaceholder')}
            disabled={submitting}
            {...form.getInputProps('password')}
          />
          <Select
            label={t('common.role')}
            data={roleOptions}
            value={form.getValues().role}
            onChange={(value) =>
              form.setFieldValue('role', (value ?? 'normal_user') as UserRole)
            }
            disabled={submitting}
            allowDeselect={false}
          />
          <Group justify="flex-end">
            <Button variant="default" onClick={editModal.close} disabled={submitting}>
              {t('common.cancel')}
            </Button>
            <Button color="wg" onClick={() => void handleEdit()} loading={submitting}>
              {t('common.save')}
            </Button>
          </Group>
        </Stack>
      </Modal>

      {/* 删除用户确认 */}
      <Modal
        opened={deleteOpened}
        onClose={deleteModal.close}
        title={t('users.deleteUser')}
        radius="sm"
      >
        <Stack gap="md">
          <Alert color="red" variant="light" icon={<IconAlertTriangle size={18} />} radius="xs">
            {t('users.deleteConfirm')}
          </Alert>
          {selected ? (
            <Stack gap={6}>
              <Group gap={6}>
                <Text size="sm" c="dimmed">
                  {t('common.name')}:
                </Text>
                <Text size="sm" fw={600}>
                  {selected.name}
                </Text>
              </Group>
              <Group gap={6}>
                <Text size="sm" c="dimmed">
                  {t('common.email')}:
                </Text>
                <Text size="sm" fw={600}>
                  {selected.email}
                </Text>
              </Group>
            </Stack>
          ) : null}
          <Group justify="flex-end">
            <Button variant="default" onClick={deleteModal.close} disabled={submitting}>
              {t('common.cancel')}
            </Button>
            <Button color="red" onClick={() => void handleDelete()} loading={submitting}>
              {t('common.delete')}
            </Button>
          </Group>
        </Stack>
      </Modal>
    </Stack>
  );
}
